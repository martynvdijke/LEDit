import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';

/// Compile-time telemetry defaults.
///
/// Nothing here is required: the runtime OTLP endpoint comes from the server
/// (`GET /api/otel-settings`), and the values below only act as fallbacks for
/// builds that want to pin an endpoint or analytics host locally.
abstract final class TelemetryConfig {
  static const otelEndpoint = String.fromEnvironment(
    'OTEL_EXPORTER_OTLP_ENDPOINT',
  );
  static const otelHeaders = String.fromEnvironment(
    'OTEL_EXPORTER_OTLP_HEADERS',
  );
  static const serviceName = String.fromEnvironment(
    'OTEL_SERVICE_NAME',
    defaultValue: 'ledit-android',
  );
  static const serviceVersion = String.fromEnvironment(
    'OTEL_SERVICE_VERSION',
    defaultValue: '1.0.0',
  );
  static const umamiUrl = String.fromEnvironment('UMAMI_URL');
  static const umamiWebsiteId = String.fromEnvironment('UMAMI_WEBSITE_ID');

  static bool get otelEnabled =>
      otelEndpoint.trim().isNotEmpty && !kIsWeb;
  static bool get umamiEnabled =>
      umamiUrl.trim().isNotEmpty && umamiWebsiteId.trim().isNotEmpty;
}

/// Allowlisted analytics events. Keys and values are enforced at the boundary,
/// so free-form user data can never be sent.
const Map<String, Set<String>> umamiEventAllowlist = {
  'screen_view': {'screen'},
  'theme_changed': {'theme'},
  'connection_saved': {'mode'},
};

const Set<String> _screens = {
  'dashboard',
  'settings',
  'onboarding',
  'devices',
  'playlists',
  'scenes',
  'gallery',
  'logs',
};
const Set<String> _themes = {'system', 'light', 'dark'};
const Set<String> _modes = {'oidc', 'api_token', 'password', 'oidc_cookie'};

Map<String, Object>? sanitizeUmamiEvent(
  String event, [
  Map<String, Object?> props = const {},
]) {
  final allowed = umamiEventAllowlist[event];
  if (allowed == null) return null;
  final clean = <String, Object>{};
  for (final entry in props.entries) {
    if (!allowed.contains(entry.key)) continue;
    final value = entry.value;
    if (value is bool || value is num) {
      clean[entry.key] = value as Object;
      continue;
    }
    if (value is! String || value.length > 64) continue;
    if (entry.key == 'screen' && !_screens.contains(value)) continue;
    if (entry.key == 'theme' && !_themes.contains(value)) continue;
    if (entry.key == 'mode' && !_modes.contains(value)) continue;
    clean[entry.key] = value;
  }
  if (allowed.isNotEmpty && clean.isEmpty) return null;
  return clean;
}

/// Runtime OTLP configuration as published by the LEDit server.
class OtelSettings {
  const OtelSettings({
    required this.endpoint,
    required this.enabled,
    required this.tracesEnabled,
    this.metricsEnabled = false,
    this.logsEnabled = false,
  });

  /// Disabled settings, used when the server has no OTel row.
  static const disabled = OtelSettings(
    endpoint: '',
    enabled: false,
    tracesEnabled: false,
  );

  final String endpoint;
  final bool enabled;
  final bool tracesEnabled;
  final bool metricsEnabled;
  final bool logsEnabled;

  /// Tolerant parser: accepts either the short (`endpoint`) or the persisted
  /// (`otel_endpoint`) spelling, and treats a missing `traces_enabled` as
  /// following `enabled`. An empty endpoint always disables tracing.
  factory OtelSettings.fromJson(Map<String, dynamic> json) {
    String readString(List<String> keys) {
      for (final key in keys) {
        final value = json[key];
        if (value is String && value.trim().isNotEmpty) return value.trim();
      }
      return '';
    }

    bool readBool(List<String> keys, bool fallback) {
      for (final key in keys) {
        final value = json[key];
        if (value is bool) return value;
        if (value is num) return value != 0;
        if (value is String) {
          final normalized = value.trim().toLowerCase();
          if (normalized == 'true' || normalized == '1') return true;
          if (normalized == 'false' || normalized == '0') return false;
        }
      }
      return fallback;
    }

    final endpoint = readString(['endpoint', 'otel_endpoint']);
    final enabled = readBool(['enabled', 'otel_enabled'], false);
    final traces = readBool(['traces_enabled'], enabled) && endpoint.isNotEmpty;
    return OtelSettings(
      endpoint: endpoint,
      enabled: enabled && endpoint.isNotEmpty,
      tracesEnabled: traces,
      metricsEnabled:
          readBool(['metrics_enabled'], false) && endpoint.isNotEmpty,
      logsEnabled: readBool(['logs_enabled'], false) && endpoint.isNotEmpty,
    );
  }

  Map<String, Object> toJson() => {
        'endpoint': endpoint,
        'enabled': enabled,
        'traces_enabled': tracesEnabled,
        'metrics_enabled': metricsEnabled,
        'logs_enabled': logsEnabled,
      };
}

/// Fetches OTel settings from a LEDit server.
///
/// Never throws: network errors, timeouts and non-200 responses all yield
/// `null`, because telemetry must never affect app behaviour.
Future<OtelSettings?> fetchOtelSettings(
  String baseUrl, {
  Dio? dio,
  Duration timeout = const Duration(seconds: 3),
}) async {
  final base = baseUrl.trim().replaceAll(RegExp(r'/+$'), '');
  if (base.isEmpty) return null;

  final client = dio ??
      Dio(
        BaseOptions(
          connectTimeout: timeout,
          receiveTimeout: timeout,
          sendTimeout: timeout,
        ),
      );
  final owned = dio == null;
  try {
    final response = await client.get<dynamic>(
      '$base/api/otel-settings',
      options: Options(
        responseType: ResponseType.json,
        validateStatus: (_) => true,
      ),
    );
    if (response.statusCode != 200) return null;
    final data = response.data;
    if (data is Map) {
      return OtelSettings.fromJson(Map<String, dynamic>.from(data));
    }
    if (data is String && data.trim().isNotEmpty) {
      final decoded = jsonDecode(data);
      if (decoded is Map) {
        return OtelSettings.fromJson(Map<String, dynamic>.from(decoded));
      }
    }
    return null;
  } catch (_) {
    return null;
  } finally {
    if (owned) client.close(force: true);
  }
}

String randomHex(int bytes) {
  final random = Random.secure();
  final buffer = StringBuffer();
  for (var i = 0; i < bytes; i++) {
    buffer.write(random.nextInt(256).toRadixString(16).padLeft(2, '0'));
  }
  return buffer.toString();
}

String buildTraceparent(String traceId, String spanId) =>
    '00-$traceId-$spanId-01';

class TraceSpan {
  TraceSpan({
    required this.name,
    required this.attributes,
    required this.traceId,
    required this.spanId,
    required this.startNanos,
    this.kind = 1,
  });

  final String name;
  final Map<String, Object> attributes;
  final String traceId;
  final String spanId;
  final int startNanos;
  final int kind;
  bool _ended = false;
  int? statusCode;
  String error = '';

  String get traceparent => buildTraceparent(traceId, spanId);

  void end({
    int? httpStatus,
    Object? failure,
    Map<String, Object> extra = const {},
  }) {
    if (_ended) return;
    _ended = true;
    statusCode = httpStatus;
    if (failure != null) error = failure.toString();
    final attrs = <String, Object>{...attributes, ...extra};
    if (httpStatus != null) attrs['http.response.status_code'] = httpStatus;
    if (error.isNotEmpty) attrs['error.message'] = _short(error);
    Telemetry.instance._finish(
      this,
      attrs,
      error.isNotEmpty || (httpStatus != null && httpStatus >= 500),
    );
  }

  String _short(String value) =>
      value.length > 200 ? '${value.substring(0, 200)}…' : value;
}

List<Map<String, Object?>> _otlpAttributes(Map<String, Object> values) =>
    values.entries
        .map(
          (entry) => {
            'key': entry.key,
            'value': switch (entry.value) {
              final bool value => {'boolValue': value},
              final int value => {'intValue': value.toString()},
              final double value => {'doubleValue': value},
              _ => {'stringValue': entry.value.toString()},
            },
          },
        )
        .toList();

Map<String, Object?> buildOtlpPayload({
  required String serviceName,
  required String serviceVersion,
  required TraceSpan span,
  required Map<String, Object> attributes,
  required bool failed,
  required int endNanos,
}) {
  return {
    'resourceSpans': [
      {
        'resource': {
          'attributes': _otlpAttributes({
            'service.name': serviceName,
            'service.version': serviceVersion,
            'telemetry.sdk.name': 'ledit-mobile',
            'telemetry.sdk.language': 'dart',
          }),
        },
        'scopeSpans': [
          {
            'scope': {'name': 'ledit.mobile', 'version': serviceVersion},
            'spans': [
              {
                'traceId': span.traceId,
                'spanId': span.spanId,
                'name': span.name,
                'kind': span.kind,
                'startTimeUnixNano': span.startNanos.toString(),
                'endTimeUnixNano': endNanos.toString(),
                'attributes': _otlpAttributes(attributes),
                'status': {'code': failed ? 2 : 1},
              },
            ],
          },
        ],
      },
    ],
  };
}

class Telemetry {
  Telemetry._();

  static final Telemetry instance = Telemetry._();

  Dio? _client;
  Timer? _timer;
  final List<Map<String, Object?>> _queue = [];
  bool _started = false;
  String _tracesEndpoint = '';
  Map<String, String> _headers = const {};

  bool get otelEnabled => _tracesEndpoint.isNotEmpty;
  bool get umamiEnabled => TelemetryConfig.umamiEnabled;

  /// Starts trace export, preferring an explicit [endpoint] over the
  /// compile-time fallback. Safe to call repeatedly.
  Future<void> start({
    String? endpoint,
    Map<String, String>? headers,
  }) async {
    if (_started) return;
    _started = true;

    if (kIsWeb) return;
    final raw = (endpoint ?? '').trim().isNotEmpty
        ? endpoint!.trim()
        : TelemetryConfig.otelEndpoint.trim();
    if (raw.isEmpty) return;

    _client = Dio(
      BaseOptions(
        connectTimeout: const Duration(seconds: 5),
        receiveTimeout: const Duration(seconds: 10),
        sendTimeout: const Duration(seconds: 10),
      ),
    );
    _tracesEndpoint = normalizeEndpoint(raw);
    _headers = headers ?? parseOtelHeaders(TelemetryConfig.otelHeaders);
    _timer = Timer.periodic(const Duration(seconds: 5), (_) => _flush());

    final span = beginSpan('app.start');
    span.end();
  }

  /// Resolves the trace endpoint from [baseUrl]'s server and starts exporting.
  ///
  /// Failures are swallowed: an offline device simply exports nothing.
  Future<void> startFromServer(String baseUrl) async {
    if (_started) return;
    final settings = await fetchOtelSettings(baseUrl);
    if (settings == null || !settings.tracesEnabled) return;
    await start(endpoint: settings.endpoint);
  }

  TraceSpan beginSpan(
    String name, {
    Map<String, Object> attributes = const {},
    int kind = 1,
  }) {
    return TraceSpan(
      name: name,
      attributes: attributes,
      traceId: randomHex(16),
      spanId: randomHex(8),
      startNanos: DateTime.now().microsecondsSinceEpoch * 1000,
      kind: kind,
    );
  }

  void screen(String name) {
    final span = beginSpan('screen $name', attributes: {'screen.name': name});
    span.end();
    track('screen_view', {'screen': name});
  }

  void track(String event, [Map<String, Object?> props = const {}]) {
    final clean = sanitizeUmamiEvent(event, props);
    if (clean == null || !TelemetryConfig.umamiEnabled) return;
    final base = TelemetryConfig.umamiUrl.replaceAll(RegExp(r'/+$'), '');
    final uri = Uri.tryParse('$base/api/send');
    if (uri == null) return;
    final body = {
      'type': 'event',
      'payload': {
        'website': TelemetryConfig.umamiWebsiteId,
        'hostname': 'ledit.app',
        'url': '/app/$event',
        'event': event,
        'data': clean,
      },
    };
    final client = _client ??= Dio();
    unawaited(
      client
          .postUri<dynamic>(
            uri,
            data: jsonEncode(body),
            options: Options(
              contentType: Headers.jsonContentType,
              validateStatus: (_) => true,
            ),
          )
          .then((_) {}, onError: (Object _) {}),
    );
  }

  void _finish(TraceSpan span, Map<String, Object> attributes, bool failed) {
    if (!otelEnabled) return;
    final payload = buildOtlpPayload(
      serviceName: TelemetryConfig.serviceName,
      serviceVersion: TelemetryConfig.serviceVersion,
      span: span,
      attributes: attributes,
      failed: failed,
      endNanos: DateTime.now().microsecondsSinceEpoch * 1000,
    );
    _queue.add(payload);
    if (_queue.length >= 20) _flush();
  }

  void _flush() {
    final client = _client;
    if (_queue.isEmpty || client == null || _tracesEndpoint.isEmpty) return;

    final spans = <Map<String, Object?>>[];
    for (final payload in _queue) {
      final resourceSpans = payload['resourceSpans'];
      if (resourceSpans is List && resourceSpans.isNotEmpty) {
        final resource = resourceSpans.first;
        if (resource is Map) {
          final scopeSpans = resource['scopeSpans'];
          if (scopeSpans is List && scopeSpans.isNotEmpty) {
            final scope = scopeSpans.first;
            if (scope is Map && scope['spans'] is List) {
              spans.addAll(
                (scope['spans'] as List).cast<Map<String, Object?>>(),
              );
            }
          }
        }
      }
    }
    _queue.clear();
    if (spans.isEmpty) return;

    final payload = {
      'resourceSpans': [
        {
          'resource': {
            'attributes': _otlpAttributes({
              'service.name': TelemetryConfig.serviceName,
              'service.version': TelemetryConfig.serviceVersion,
              'telemetry.sdk.name': 'ledit-mobile',
              'telemetry.sdk.language': 'dart',
            }),
          },
          'scopeSpans': [
            {
              'scope': {
                'name': 'ledit.mobile',
                'version': TelemetryConfig.serviceVersion,
              },
              'spans': spans,
            },
          ],
        },
      ],
    };

    unawaited(
      client
          .post<dynamic>(
            _tracesEndpoint,
            data: jsonEncode(payload),
            options: Options(
              contentType: Headers.jsonContentType,
              headers: _headers,
              validateStatus: (_) => true,
            ),
          )
          .then(
            (response) {
              final code = response.statusCode ?? 0;
              if (code >= 400 && kDebugMode) {
                debugPrint('OTLP export failed: $code');
              }
            },
            onError: (Object error) {
              if (kDebugMode) debugPrint('OTLP export failed: $error');
            },
          ),
    );
  }

  Future<void> shutdown() async {
    _timer?.cancel();
    _flush();
    await Future<void>.delayed(const Duration(milliseconds: 200));
    _client?.close(force: true);
    _client = null;
    _started = false;
  }

  /// Appends `/v1/traces` unless the endpoint already points at it.
  static String normalizeEndpoint(String raw) {
    final base = raw.trim().replaceAll(RegExp(r'/+$'), '');
    if (base.isEmpty) return '';
    if (base.endsWith('/v1/traces')) return base;
    return '$base/v1/traces';
  }

  static Map<String, String> parseOtelHeaders(String raw) {
    final out = <String, String>{};
    for (final pair in raw.split(',')) {
      final idx = pair.indexOf('=');
      if (idx <= 0) continue;
      final key = pair.substring(0, idx).trim();
      var value = pair.substring(idx + 1).trim();
      try {
        value = Uri.decodeComponent(value);
      } catch (_) {}
      if (key.isNotEmpty && value.isNotEmpty) out[key] = value;
    }
    return out;
  }
}
