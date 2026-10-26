import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:web_socket_channel/io.dart';

import 'api_client.dart';
import 'models.dart';
import 'preview_frame.dart';

/// Secure persistence for the connection settings.
class SessionStore {
  SessionStore(this._storage);

  static const _kServerUrl = 'ledit.server_url';
  static const _kMode = 'ledit.auth_mode';
  static const _kToken = 'ledit.api_token';
  static const _kCookie = 'ledit.session_cookie';
  static const _kUsername = 'ledit.username';

  final FlutterSecureStorage _storage;

  Future<SessionConfig?> load() async {
    final url = await _storage.read(key: _kServerUrl);
    if (url == null || url.isEmpty) return null;
    final mode = AuthMode.fromName(await _storage.read(key: _kMode));
    return SessionConfig(
      serverUrl: url,
      mode: mode,
      apiToken: await _storage.read(key: _kToken),
      sessionCookie: await _storage.read(key: _kCookie),
      username: await _storage.read(key: _kUsername),
    );
  }

  Future<void> save(SessionConfig config) async {
    await _storage.write(key: _kServerUrl, value: config.serverUrl);
    await _storage.write(key: _kMode, value: config.mode.name);
    await _storage.write(key: _kToken, value: config.apiToken ?? '');
    await _storage.write(key: _kCookie, value: config.sessionCookie ?? '');
    await _storage.write(key: _kUsername, value: config.username ?? '');
  }

  Future<void> clear() async {
    for (final key in [_kServerUrl, _kMode, _kToken, _kCookie, _kUsername]) {
      await _storage.delete(key: key);
    }
  }
}

final sessionStoreProvider = Provider<SessionStore>(
  (ref) => SessionStore(const FlutterSecureStorage()),
);

/// Holds the active server + credential pair, or null when onboarding is due.
class SessionController extends AsyncNotifier<SessionConfig?> {
  @override
  Future<SessionConfig?> build() => ref.read(sessionStoreProvider).load();

  Future<void> save(SessionConfig config) async {
    await ref.read(sessionStoreProvider).save(config);
    state = AsyncData(config);
  }

  Future<void> signOut() async {
    await ref.read(sessionStoreProvider).clear();
    state = const AsyncData(null);
  }
}

final sessionControllerProvider =
    AsyncNotifierProvider<SessionController, SessionConfig?>(SessionController.new);

/// REST client for the active session; null before onboarding completes.
final apiProvider = Provider<LeditApi?>((ref) {
  final session = ref.watch(sessionControllerProvider).value;
  if (session == null || session.baseUrl.isEmpty) return null;
  return LeditApi(baseUrl: session.baseUrl, headers: session.authHeaders);
});

final devicesProvider = FutureProvider<List<Device>>((ref) async {
  final api = ref.watch(apiProvider);
  if (api == null) return const [];
  return api.devices();
});

final playlistsProvider = FutureProvider<List<PlaylistInfo>>((ref) async {
  final api = ref.watch(apiProvider);
  if (api == null) return const [];
  return api.playlists();
});

final scenesProvider = FutureProvider<List<SceneInfo>>((ref) async {
  final api = ref.watch(apiProvider);
  if (api == null) return const [];
  return api.scenes();
});

final imagesProvider = FutureProvider<List<ImageInfo>>((ref) async {
  final api = ref.watch(apiProvider);
  if (api == null) return const [];
  return api.images();
});

final feedStatusProvider = FutureProvider<FeedStatus>((ref) async {
  final api = ref.watch(apiProvider);
  if (api == null) return const FeedStatus(paused: false);
  return api.feedStatus();
});

/// Live frame stream for one device, straight from the server's preview feed.
///
/// The server owns cadence (one frame per refresh interval) and never writes
/// device liveness for preview connections, so this is safe to leave open.
final devicePreviewProvider =
    StreamProvider.autoDispose.family<PreviewFrame, int>((ref, deviceId) {
  final session = ref.watch(sessionControllerProvider).value;
  final api = ref.watch(apiProvider);
  if (session == null || api == null) {
    return const Stream<PreviewFrame>.empty();
  }
  final channel = IOWebSocketChannel.connect(
    api.previewUri(deviceId),
    headers: session.authHeaders,
    pingInterval: const Duration(seconds: 30),
  );
  ref.onDispose(() => channel.sink.close());
  return channel.stream
      .map(decodePreviewFrame)
      .where((frame) => frame != null)
      .cast<PreviewFrame>();
});

/// Refreshes every server-side list after a mutation.
Future<void> refreshServerState(WidgetRef ref) async {
  ref.invalidate(devicesProvider);
  ref.invalidate(playlistsProvider);
  ref.invalidate(scenesProvider);
  ref.invalidate(feedStatusProvider);
  await ref.read(devicesProvider.future);
}
