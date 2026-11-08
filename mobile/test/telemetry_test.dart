import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:ledit/src/telemetry/telemetry.dart';

/// Answers every request from a scripted handler and records what was asked.
class _FakeAdapter implements HttpClientAdapter {
  _FakeAdapter(this.handler);

  final Future<ResponseBody> Function(RequestOptions options) handler;
  final List<RequestOptions> requests = [];

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) {
    requests.add(options);
    return handler(options);
  }

  @override
  void close({bool force = false}) {}
}

ResponseBody _json(Object body, {int status = 200}) => ResponseBody.fromString(
      jsonEncode(body),
      status,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );

Dio _dioWith(_FakeAdapter adapter) => Dio()..httpClientAdapter = adapter;

void main() {
  group('normalizeEndpoint', () {
    test('appends /v1/traces', () {
      expect(
        Telemetry.normalizeEndpoint('https://otel.example.com'),
        'https://otel.example.com/v1/traces',
      );
    });

    test('strips trailing slashes and keeps an explicit trace path', () {
      expect(
        Telemetry.normalizeEndpoint(' https://otel.example.com/ '),
        'https://otel.example.com/v1/traces',
      );
      expect(
        Telemetry.normalizeEndpoint('https://otel.example.com/v1/traces/'),
        'https://otel.example.com/v1/traces',
      );
    });

    test('empty stays empty', () {
      expect(Telemetry.normalizeEndpoint('  '), '');
    });
  });

  group('parseOtelHeaders', () {
    test('parses comma separated key=value pairs', () {
      expect(
        Telemetry.parseOtelHeaders('authorization=Bearer%20abc,x-tenant=ledit'),
        {'authorization': 'Bearer abc', 'x-tenant': 'ledit'},
      );
    });

    test('skips malformed entries and empty values', () {
      expect(
        Telemetry.parseOtelHeaders('novalue,=blank,key=, good = ok '),
        {'good': 'ok'},
      );
    });

    test('empty input yields no headers', () {
      expect(Telemetry.parseOtelHeaders(''), isEmpty);
    });
  });

  group('OtelSettings.fromJson', () {
    test('reads short keys', () {
      final settings = OtelSettings.fromJson({
        'endpoint': 'https://otel.example.com',
        'enabled': true,
        'traces_enabled': true,
        'metrics_enabled': false,
        'logs_enabled': false,
      });
      expect(settings.endpoint, 'https://otel.example.com');
      expect(settings.enabled, isTrue);
      expect(settings.tracesEnabled, isTrue);
    });

    test('reads persisted otel_ keys and derives traces from enabled', () {
      final settings = OtelSettings.fromJson({
        'otel_endpoint': 'https://collector:4318',
        'otel_enabled': true,
      });
      expect(settings.endpoint, 'https://collector:4318');
      expect(settings.tracesEnabled, isTrue);
    });

    test('missing traces_enabled falls back to enabled', () {
      final settings = OtelSettings.fromJson({
        'endpoint': 'https://otel.example.com',
        'enabled': true,
      });
      expect(settings.tracesEnabled, isTrue);
    });

    test('empty endpoint disables everything', () {
      final settings = OtelSettings.fromJson({
        'endpoint': '',
        'enabled': true,
        'traces_enabled': true,
      });
      expect(settings.enabled, isFalse);
      expect(settings.tracesEnabled, isFalse);
      expect(settings.metricsEnabled, isFalse);
      expect(settings.logsEnabled, isFalse);
    });

    test('tolerates strings and numbers for booleans', () {
      final settings = OtelSettings.fromJson({
        'endpoint': 'https://otel.example.com',
        'enabled': 'true',
        'traces_enabled': 1,
      });
      expect(settings.enabled, isTrue);
      expect(settings.tracesEnabled, isTrue);
    });

    test('empty json yields disabled settings', () {
      final settings = OtelSettings.fromJson(const {});
      expect(settings.endpoint, isEmpty);
      expect(settings.enabled, isFalse);
      expect(settings.tracesEnabled, isFalse);
    });
  });

  group('fetchOtelSettings', () {
    test('returns parsed settings on 200', () async {
      final adapter = _FakeAdapter(
        (_) async => _json({
          'endpoint': 'https://otel.example.com',
          'enabled': true,
          'traces_enabled': true,
          'metrics_enabled': false,
          'logs_enabled': false,
        }),
      );
      final settings = await fetchOtelSettings(
        'https://ledit.example.com/',
        dio: _dioWith(adapter),
      );

      expect(settings, isNotNull);
      expect(settings!.endpoint, 'https://otel.example.com');
      expect(settings.tracesEnabled, isTrue);
      expect(adapter.requests.single.uri.toString(),
          'https://ledit.example.com/api/otel-settings');
    });

    test('returns null on a non-200 response', () async {
      final adapter = _FakeAdapter((_) async => _json({'error': 'nope'}, status: 500));
      final settings = await fetchOtelSettings(
        'https://ledit.example.com',
        dio: _dioWith(adapter),
      );
      expect(settings, isNull);
    });

    test('returns null instead of throwing', () async {
      final adapter = _FakeAdapter(
        (_) async => throw DioException(
          requestOptions: RequestOptions(path: '/api/otel-settings'),
          error: 'offline',
        ),
      );
      final settings = await fetchOtelSettings(
        'https://ledit.example.com',
        dio: _dioWith(adapter),
      );
      expect(settings, isNull);
    });

    test('returns null for an empty base url', () async {
      expect(await fetchOtelSettings('  '), isNull);
    });
  });

  group('buildOtlpPayload', () {
    test('produces a valid OTLP resourceSpans envelope', () {
      final span = TraceSpan(
        name: 'app.start',
        attributes: const {},
        traceId: 'a'.padRight(32, 'a'),
        spanId: 'b'.padRight(16, 'b'),
        startNanos: 1000,
      );

      final payload = buildOtlpPayload(
        serviceName: 'ledit-android',
        serviceVersion: '1.2.3',
        span: span,
        attributes: const {'http.method': 'GET', 'retry': 2, 'latency_ms': 3.5},
        failed: false,
        endNanos: 2000,
      );

      final resourceSpans = (payload['resourceSpans'] as List).single as Map;
      final resource = resourceSpans['resource'] as Map;
      final resourceAttrs = resource['attributes'] as List;
      final keys = resourceAttrs
          .map((attr) => (attr as Map)['key'])
          .toList();
      expect(keys, containsAll(['service.name', 'telemetry.sdk.name']));
      expect(
        resourceAttrs.firstWhere((attr) => (attr as Map)['key'] == 'service.name')
            ['value'],
        {'stringValue': 'ledit-android'},
      );

      final scope = (resourceSpans['scopeSpans'] as List).single as Map;
      expect((scope['scope'] as Map)['name'], 'ledit.mobile');

      final otlpSpan = (scope['spans'] as List).single as Map;
      expect(otlpSpan['traceId'], 'a'.padRight(32, 'a'));
      expect(otlpSpan['spanId'], 'b'.padRight(16, 'b'));
      expect(otlpSpan['name'], 'app.start');
      expect(otlpSpan['kind'], 1);
      expect(otlpSpan['startTimeUnixNano'], '1000');
      expect(otlpSpan['endTimeUnixNano'], '2000');
      expect(otlpSpan['status'], {'code': 1});

      final attrs = {
        for (final attr in otlpSpan['attributes'] as List)
          (attr as Map)['key']: attr['value'],
      };
      expect(attrs['http.method'], {'stringValue': 'GET'});
      expect(attrs['retry'], {'intValue': '2'});
      expect(attrs['latency_ms'], {'doubleValue': 3.5});
    });

    test('marks failed spans with error status', () {
      final span = TraceSpan(
        name: 'request',
        attributes: const {},
        traceId: 'a'.padRight(32, 'a'),
        spanId: 'b'.padRight(16, 'b'),
        startNanos: 1,
      );
      final payload = buildOtlpPayload(
        serviceName: 'ledit-android',
        serviceVersion: '1.0.0',
        span: span,
        attributes: const {},
        failed: true,
        endNanos: 2,
      );
      final scope = ((payload['resourceSpans'] as List).single
          as Map)['scopeSpans'] as List;
      final otlpSpan = ((scope.single as Map)['spans'] as List).single as Map;
      expect(otlpSpan['status'], {'code': 2});
    });
  });
}
