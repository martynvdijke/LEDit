import 'dart:convert';

import 'package:dio/dio.dart';

import 'models.dart';

/// How the app authenticates against the LEDit server.
///
/// LEDit accepts either a session cookie (browser login / OIDC callback) or an
/// API token as a bearer credential on every `/api/*` route. The OIDC flow
/// itself is a browser redirect, so the app captures the resulting session
/// cookie instead of driving the IdP.
enum AuthMode {
  apiToken('API token'),
  password('Username & password'),
  oidcCookie('OIDC session cookie');

  const AuthMode(this.label);

  final String label;

  static AuthMode fromName(String? name) {
    return AuthMode.values.firstWhere(
      (m) => m.name == name,
      orElse: () => AuthMode.apiToken,
    );
  }
}

/// Immutable connection settings persisted in secure storage.
class SessionConfig {
  const SessionConfig({
    required this.serverUrl,
    required this.mode,
    this.apiToken,
    this.sessionCookie,
    this.username,
  });

  final String serverUrl;
  final AuthMode mode;
  final String? apiToken;
  final String? sessionCookie;
  final String? username;

  /// Normalised base URL without a trailing slash and with a scheme.
  String get baseUrl => normaliseServerUrl(serverUrl);

  /// WebSocket origin (`http` -> `ws`, `https` -> `wss`).
  String get wsBaseUrl => wsBaseUrlFor(serverUrl);

  Map<String, String> get authHeaders => authHeadersFor(mode, apiToken, sessionCookie);

  String? get credential => mode == AuthMode.apiToken ? apiToken : sessionCookie;
}

/// Adds a scheme when the user typed a bare host and strips trailing slashes.
String normaliseServerUrl(String raw) {
  var url = raw.trim();
  while (url.endsWith('/')) {
    url = url.substring(0, url.length - 1);
  }
  if (url.isEmpty) return url;
  if (!url.startsWith('http://') && !url.startsWith('https://')) {
    url = 'http://$url';
  }
  return url;
}

/// `http`/`https` origin rewritten to `ws`/`wss` for WebSocket routes.
String wsBaseUrlFor(String baseUrl) {
  final base = normaliseServerUrl(baseUrl);
  if (base.startsWith('https://')) {
    return 'wss://${base.substring('https://'.length)}';
  }
  if (base.startsWith('http://')) {
    return 'ws://${base.substring('http://'.length)}';
  }
  return 'ws://$base';
}

/// Bearer header for API tokens, `session` cookie header otherwise.
Map<String, String> authHeadersFor(
  AuthMode mode,
  String? apiToken,
  String? sessionCookie,
) {
  if (mode == AuthMode.apiToken) {
    if (apiToken == null || apiToken.isEmpty) return const {};
    return {'Authorization': 'Bearer $apiToken'};
  }
  if (sessionCookie == null || sessionCookie.isEmpty) return const {};
  return {'Cookie': 'session=$sessionCookie'};
}

/// Extracts the `session` cookie value from a `Set-Cookie` header.
String? sessionCookieFromSetCookie(String? setCookie) {
  if (setCookie == null || setCookie.isEmpty) return null;
  for (final part in setCookie.split(';')) {
    final trimmed = part.trim();
    if (trimmed.startsWith('session=')) {
      final value = trimmed.substring('session='.length);
      if (value.isNotEmpty) return value;
    }
  }
  return null;
}

class ApiException implements Exception {
  ApiException(this.message, {this.statusCode});

  final String message;
  final int? statusCode;

  @override
  String toString() => message;
}

/// Thin REST client over the LEDit control API.
class LeditApi {
  LeditApi({
    required this.baseUrl,
    this.headers = const {},
    Dio? dio,
  }) : _dio = dio ?? Dio();

  final String baseUrl;
  final Map<String, String> headers;
  final Dio _dio;

  static const previewPath = '/ws/device';
  static const imageUploadPath = '/admin/datasources/images/new';

  Uri previewUri(int deviceId) =>
      Uri.parse('${wsBaseUrlFor(baseUrl)}$previewPath/$deviceId/preview');

  Options get _options => Options(headers: headers, validateStatus: (_) => true);

  Map<String, dynamic> _decode(Response<dynamic> res) {
    final data = res.data;
    if (data is Map<String, dynamic>) return data;
    if (data is String && data.isNotEmpty) {
      try {
        final decoded = jsonDecode(data);
        if (decoded is Map<String, dynamic>) return decoded;
      } catch (_) {
        // fall through to the generic error below
      }
    }
    return const {};
  }

  Never _fail(Response<dynamic> res) {
    final body = _decode(res);
    final message = (body['error'] as String?) ??
        '${res.requestOptions.method} ${res.requestOptions.path} failed (${res.statusCode})';
    throw ApiException(message, statusCode: res.statusCode);
  }

  Future<Response<dynamic>> _get(String path) async {
    final res = await _dio.get<dynamic>('$baseUrl$path', options: _options);
    if (res.statusCode == null || res.statusCode! >= 400) _fail(res);
    return res;
  }

  Future<Response<dynamic>> _postJson(String path, Map<String, dynamic> body) async {
    final res = await _dio.post<dynamic>(
      '$baseUrl$path',
      data: jsonEncode(body),
      options: _options.copyWith(contentType: Headers.jsonContentType),
    );
    if (res.statusCode == null || res.statusCode! >= 400) _fail(res);
    return res;
  }

  Future<List<Device>> devices() async {
    final res = await _get('/api/devices');
    final list = (_decode(res)['devices'] as List<dynamic>?) ?? const [];
    return list
        .whereType<Map<String, dynamic>>()
        .map(Device.fromJson)
        .toList(growable: false);
  }

  Future<List<PlaylistInfo>> playlists() async {
    final res = await _get('/api/playlists');
    final list = (_decode(res)['playlists'] as List<dynamic>?) ?? const [];
    return list
        .whereType<Map<String, dynamic>>()
        .map(PlaylistInfo.fromJson)
        .toList(growable: false);
  }

  Future<List<SceneInfo>> scenes() async {
    final res = await _get('/api/scenes');
    final list = (_decode(res)['scenes'] as List<dynamic>?) ?? const [];
    return list
        .whereType<Map<String, dynamic>>()
        .map(SceneInfo.fromJson)
        .toList(growable: false);
  }

  Future<List<ImageInfo>> images() async {
    final res = await _get('/api/images');
    final list = (_decode(res)['images'] as List<dynamic>?) ?? const [];
    return list
        .whereType<Map<String, dynamic>>()
        .map(ImageInfo.fromJson)
        .toList(growable: false);
  }

  Future<FeedStatus> feedStatus() async {
    final res = await _get('/api/feed/current');
    return FeedStatus.fromJson(_decode(res));
  }

  Future<void> feedNext() => _postJson('/api/feed/next', const {});
  Future<void> feedPause() => _postJson('/api/feed/pause', const {});
  Future<void> feedResume() => _postJson('/api/feed/resume', const {});

  Future<void> setBrightness(int deviceId, int level) =>
      _postJson('/api/devices/$deviceId/brightness', {'level': level});

  /// Activates a playlist/scene or pins a rotation source on one device.
  Future<void> select(int deviceId, String target) =>
      _postJson('/api/devices/$deviceId/select', {'target': target});

  Future<void> sendMessage(int deviceId, String text, {int? ttlSeconds}) =>
      _postJson('/api/devices/$deviceId/message', {
        'text': text,
        if (ttlSeconds != null && ttlSeconds > 0) 'ttl': ttlSeconds,
      });

  /// Uploads an image into the server's image datasource library.
  ///
  /// Returns the newest library entry, which the caller can hand to
  /// [select] as `source:image:<id>`.
  Future<ImageInfo?> uploadImage(String filePath, {String? fileName}) async {
    final form = FormData.fromMap({
      'file': await MultipartFile.fromFile(filePath, filename: fileName),
    });
    final res = await _dio.post<dynamic>(
      '$baseUrl$imageUploadPath',
      data: form,
      options: Options(
        headers: headers,
        followRedirects: false,
        validateStatus: (_) => true,
      ),
    );
    final code = res.statusCode ?? 0;
    if (code >= 400) _fail(res);
    final entries = await images();
    if (entries.isEmpty) return null;
    return entries.reduce((a, b) => a.id > b.id ? a : b);
  }

  /// Exchanges username/password for a session cookie via `POST /login`.
  static Future<String> login(String baseUrl, String username, String password) async {
    final dio = Dio();
    final res = await dio.post<dynamic>(
      '${normaliseServerUrl(baseUrl)}/login',
      data: {'username': username, 'password': password},
      options: Options(
        contentType: Headers.formUrlEncodedContentType,
        validateStatus: (_) => true,
        followRedirects: false,
      ),
    );
    final cookie = sessionCookieFromSetCookie(res.headers.value('set-cookie'));
    if (cookie == null) {
      if (res.statusCode == 302 || res.statusCode == 303) {
        throw ApiException('The server rejected those credentials.');
      }
      throw ApiException('Log in failed (${res.statusCode}).');
    }
    return cookie;
  }

  static Future<bool> health(String baseUrl) async {
    try {
      final res = await Dio().get<dynamic>(
        '${normaliseServerUrl(baseUrl)}/api/health',
        options: Options(validateStatus: (_) => true),
      );
      return res.statusCode != null && res.statusCode! < 500;
    } catch (_) {
      return false;
    }
  }
}
