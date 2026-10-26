/// Data models mirroring the LEDit REST payloads used by the Android remote.
///
/// Only the fields the app renders are modelled; unknown keys are ignored so
/// the app tolerates a server that grew extra fields.
library;

class Device {
  const Device({
    required this.id,
    required this.name,
    required this.enabled,
    required this.width,
    required this.height,
    required this.liveness,
    required this.online,
    required this.live,
    required this.brightness,
    required this.contentMode,
    this.playlistId,
    this.playlistName = '',
    this.groupId,
    this.groupName = '',
    this.lastSeenAt,
  });

  final int id;
  final String name;
  final bool enabled;
  final int width;
  final int height;

  /// Server liveness verdict: `alive`, `stale` or `never`.
  final String liveness;

  /// Convenience flag for `liveness == 'alive'`.
  final bool online;

  /// Whether the server currently holds an in-process feed for this device.
  final bool live;

  final int brightness;
  final String contentMode;
  final int? playlistId;
  final String playlistName;
  final int? groupId;
  final String groupName;
  final DateTime? lastSeenAt;

  factory Device.fromJson(Map<String, dynamic> json) {
    return Device(
      id: json['id'] as int,
      name: (json['name'] as String?) ?? 'Unnamed',
      enabled: (json['enabled'] as bool?) ?? false,
      width: (json['width'] as int?) ?? 64,
      height: (json['height'] as int?) ?? 32,
      liveness: (json['liveness'] as String?) ?? 'never',
      online: (json['online'] as bool?) ?? false,
      live: (json['live'] as bool?) ?? false,
      brightness: (json['brightness'] as int?) ?? 100,
      contentMode: (json['content_mode'] as String?) ?? '',
      playlistId: json['playlist_id'] as int?,
      playlistName: (json['playlist_name'] as String?) ?? '',
      groupId: json['group_id'] as int?,
      groupName: (json['group_name'] as String?) ?? '',
      lastSeenAt: _parseTime(json['last_seen_at']),
    );
  }

  static DateTime? _parseTime(Object? raw) {
    if (raw is String && raw.isNotEmpty) {
      return DateTime.tryParse(raw);
    }
    return null;
  }
}

class PlaylistInfo {
  const PlaylistInfo({
    required this.id,
    required this.name,
    required this.enabled,
    required this.itemCount,
  });

  final int id;
  final String name;
  final bool enabled;
  final int itemCount;

  /// Grammar accepted by `POST /api/devices/:id/select`.
  String get selectTarget => 'playlist:$id';

  factory PlaylistInfo.fromJson(Map<String, dynamic> json) {
    return PlaylistInfo(
      id: json['id'] as int,
      name: (json['name'] as String?) ?? 'Unnamed',
      enabled: (json['enabled'] as bool?) ?? false,
      itemCount: (json['item_count'] as int?) ?? 0,
    );
  }
}

class SceneInfo {
  const SceneInfo({
    required this.id,
    required this.name,
    required this.enabled,
    required this.active,
    this.ttlSeconds,
  });

  final int id;
  final String name;
  final bool enabled;
  final bool active;
  final int? ttlSeconds;

  /// Grammar accepted by `POST /api/devices/:id/select`.
  String get selectTarget => 'scene:$id';

  factory SceneInfo.fromJson(Map<String, dynamic> json) {
    return SceneInfo(
      id: json['id'] as int,
      name: (json['name'] as String?) ?? 'Unnamed',
      enabled: (json['enabled'] as bool?) ?? false,
      active: (json['active'] as bool?) ?? false,
      ttlSeconds: json['ttl_seconds'] as int?,
    );
  }
}

class ImageInfo {
  const ImageInfo({required this.id, required this.path});

  final int id;
  final String path;

  String get fileName => path.split('/').last;

  factory ImageInfo.fromJson(Map<String, dynamic> json) {
    return ImageInfo(
      id: json['id'] as int,
      path: (json['path'] as String?) ?? '',
    );
  }
}

/// Global wall feed status from `GET /api/feed/current`.
class FeedStatus {
  const FeedStatus({required this.paused, this.current, this.next});

  final bool paused;
  final String? current;
  final String? next;

  factory FeedStatus.fromJson(Map<String, dynamic> json) {
    return FeedStatus(
      paused: (json['paused'] as bool?) ?? false,
      current: json['current'] as String?,
      next: json['next'] as String?,
    );
  }
}

/// A decoded frame from the device preview WebSocket
/// (`/ws/device/:id/preview`).
///
/// Notification frames carry no image; [notificationTitle]/[notificationText]
/// are then set and [png] is null.
class PreviewFrame {
  const PreviewFrame({
    this.png,
    this.source = '',
    this.notificationTitle,
    this.notificationText,
  });

  final List<int>? png;
  final String source;
  final String? notificationTitle;
  final String? notificationText;

  bool get hasImage => png != null && png!.isNotEmpty;

  bool get isNotification => source == 'NOTIFICATION';
}
