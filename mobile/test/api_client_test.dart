import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:ledit/src/api_client.dart';

/// Records every request and answers from a scripted handler.
class _RecordingAdapter implements HttpClientAdapter {
  _RecordingAdapter(this.handler);

  final ResponseBody Function(RequestOptions options) handler;
  final List<RequestOptions> requests = [];

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
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

void main() {
  late _RecordingAdapter adapter;
  late LeditApi api;

  LeditApi buildApi(ResponseBody Function(RequestOptions) handler) {
    adapter = _RecordingAdapter(handler);
    final dio = Dio()..httpClientAdapter = adapter;
    return LeditApi(
      baseUrl: 'http://ledit.local:8081',
      headers: const {'Authorization': 'Bearer tok'},
      dio: dio,
    );
  }

  test('devices() reads the fleet payload', () async {
    api = buildApi((_) => _json({
          'devices': [
            {'id': 1, 'name': 'Matrix', 'enabled': true, 'width': 64, 'height': 32, 'online': true},
          ],
        }));

    final devices = await api.devices();

    expect(devices, hasLength(1));
    expect(devices.first.name, 'Matrix');
    expect(adapter.requests.single.uri.path, '/api/devices');
    expect(adapter.requests.single.headers['Authorization'], 'Bearer tok');
  });

  test('setBrightness posts the level to the device route', () async {
    api = buildApi((_) => _json({'status': 'ok'}));

    await api.setBrightness(7, 35);

    final request = adapter.requests.single;
    expect(request.uri.path, '/api/devices/7/brightness');
    expect(jsonDecode(request.data as String), {'level': 35});
  });

  test('select posts the target grammar', () async {
    api = buildApi((_) => _json({'status': 'ok'}));

    await api.select(7, 'scene:3');

    final request = adapter.requests.single;
    expect(request.uri.path, '/api/devices/7/select');
    expect(jsonDecode(request.data as String), {'target': 'scene:3'});
  });

  test('sendMessage includes the TTL only when set', () async {
    api = buildApi((_) => _json({'status': 'ok'}));

    await api.sendMessage(7, 'Dinner time', ttlSeconds: 30);
    expect(
      jsonDecode(adapter.requests.last.data as String),
      {'text': 'Dinner time', 'ttl': 30},
    );

    await api.sendMessage(7, 'Dinner time');
    expect(jsonDecode(adapter.requests.last.data as String), {'text': 'Dinner time'});
  });

  test('errors surface the server message', () async {
    api = buildApi((_) => _json({'error': 'device not found'}, status: 404));

    expect(
      () => api.setBrightness(999, 50),
      throwsA(
        isA<ApiException>()
            .having((e) => e.message, 'message', 'device not found')
            .having((e) => e.statusCode, 'statusCode', 404),
      ),
    );
  });

  test('previewUri targets the device preview WebSocket', () {
    api = buildApi((_) => _json({}));
    expect(
      api.previewUri(4).toString(),
      'ws://ledit.local:8081/ws/device/4/preview',
    );
  });

  test('login captures the session cookie from Set-Cookie', () {
    expect(
      sessionCookieFromSetCookieFrom('session=xyz; Path=/; HttpOnly'),
      'xyz',
    );
  });

  test('server URL validation requires a real host', () {
    expect(isValidServerUrl('ledit.local:8081'), isTrue);
    expect(isValidServerUrl('https://ledit.example.com'), isTrue);
    expect(isValidServerUrl('  http://192.168.1.10:8081/  '), isTrue);
    expect(isValidServerUrl(''), isFalse);
    expect(isValidServerUrl('   '), isFalse);
    expect(isValidServerUrl('http://'), isFalse);
    expect(isValidServerUrl('https://'), isFalse);
  });
}

/// Local indirection so the assertion documents the header format the login
/// flow depends on.
String? sessionCookieFromSetCookieFrom(String header) =>
    sessionCookieFromSetCookie(header);
