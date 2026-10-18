import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:ledit/src/api_client.dart';
import 'package:ledit/src/models.dart';
import 'package:ledit/src/preview_frame.dart';

void main() {
  group('normaliseServerUrl', () {
    test('adds a scheme and trims trailing slashes', () {
      expect(normaliseServerUrl('ledit.local:8081/'), 'http://ledit.local:8081');
      expect(normaliseServerUrl(' https://matrix.example.com// '), 'https://matrix.example.com');
      expect(normaliseServerUrl(''), '');
    });
  });

  group('auth headers', () {
    test('api token mode sends a bearer credential', () {
      expect(
        authHeadersFor(AuthMode.apiToken, 'abc123', null),
        {'Authorization': 'Bearer abc123'},
      );
    });

    test('session modes send the session cookie', () {
      expect(
        authHeadersFor(AuthMode.oidcCookie, null, 'sess-value'),
        {'Cookie': 'session=sess-value'},
      );
      expect(
        authHeadersFor(AuthMode.password, null, 'sess-value'),
        {'Cookie': 'session=sess-value'},
      );
    });

    test('missing credentials produce no headers', () {
      expect(authHeadersFor(AuthMode.apiToken, null, null), isEmpty);
      expect(authHeadersFor(AuthMode.oidcCookie, null, ''), isEmpty);
    });
  });

  test('sessionCookieFromSetCookie extracts the session value', () {
    expect(
      sessionCookieFromSetCookie('session=abc123; Path=/; HttpOnly; SameSite=Lax'),
      'abc123',
    );
    expect(sessionCookieFromSetCookie('other=1; Path=/'), isNull);
    expect(sessionCookieFromSetCookie(null), isNull);
  });

  test('wsBaseUrlFor rewrites the scheme', () {
    expect(wsBaseUrlFor('https://matrix.example.com'), 'wss://matrix.example.com');
    expect(wsBaseUrlFor('http://ledit.local:8081'), 'ws://ledit.local:8081');
    expect(
      const SessionConfig(serverUrl: 'ledit.local:8081', mode: AuthMode.apiToken).wsBaseUrl,
      'ws://ledit.local:8081',
    );
  });

  test('AuthMode.fromName falls back to apiToken', () {
    expect(AuthMode.fromName('password'), AuthMode.password);
    expect(AuthMode.fromName('oidcCookie'), AuthMode.oidcCookie);
    expect(AuthMode.fromName('nonsense'), AuthMode.apiToken);
    expect(AuthMode.fromName(null), AuthMode.apiToken);
  });

  group('Device.fromJson', () {
    test('parses the fleet payload', () {
      final device = Device.fromJson({
        'id': 3,
        'name': 'Kitchen',
        'enabled': true,
        'width': 64,
        'height': 32,
        'liveness': 'alive',
        'online': true,
        'live': true,
        'brightness': 42,
        'content_mode': 'playlist',
        'playlist_id': 7,
        'playlist_name': 'Morning',
        'group_id': 2,
        'group_name': 'Ground floor',
        'last_seen_at': '2026-10-18T10:00:00Z',
      });

      expect(device.id, 3);
      expect(device.name, 'Kitchen');
      expect(device.online, isTrue);
      expect(device.brightness, 42);
      expect(device.playlistName, 'Morning');
      expect(device.groupName, 'Ground floor');
      expect(device.lastSeenAt?.toUtc().hour, 10);
    });

    test('tolerates missing optional fields', () {
      final device = Device.fromJson({'id': 1});
      expect(device.width, 64);
      expect(device.height, 32);
      expect(device.liveness, 'never');
      expect(device.online, isFalse);
      expect(device.lastSeenAt, isNull);
    });
  });

  test('playlist and scene select targets use the server grammar', () {
    final playlist = PlaylistInfo.fromJson({'id': 4, 'name': 'Evening', 'enabled': true, 'item_count': 3});
    expect(playlist.selectTarget, 'playlist:4');
    expect(playlist.itemCount, 3);

    final scene = SceneInfo.fromJson({'id': 9, 'name': 'Sunset', 'enabled': true, 'active': true, 'ttl_seconds': 60});
    expect(scene.selectTarget, 'scene:9');
    expect(scene.active, isTrue);
    expect(scene.ttlSeconds, 60);
  });

  test('FeedStatus maps the wall state', () {
    final status = FeedStatus.fromJson({'paused': true, 'current': 'CLOCK:1', 'next': 'RSS:2'});
    expect(status.paused, isTrue);
    expect(status.current, 'CLOCK:1');
    expect(status.next, 'RSS:2');
  });

  group('decodePreviewFrame', () {
    test('decodes a base64 image frame', () {
      final bytes = [1, 2, 3];
      final frame = decodePreviewFrame(jsonEncode({
        'format': 'PNG',
        'source': 'CLOCK:1',
        'image': base64Encode(bytes),
      }));

      expect(frame, isNotNull);
      expect(frame!.png, bytes);
      expect(frame.source, 'CLOCK:1');
      expect(frame.isNotification, isFalse);
    });

    test('decodes notification frames without an image', () {
      final frame = decodePreviewFrame(jsonEncode({
        'format': 'PNG',
        'source': 'NOTIFICATION',
        'title': 'Alert',
        'message': 'Someone is at the door',
      }));

      expect(frame!.hasImage, isFalse);
      expect(frame.isNotification, isTrue);
      expect(frame.notificationText, 'Someone is at the door');
    });

    test('accepts binary websocket payloads', () {
      final frame = decodePreviewFrame(utf8.encode(jsonEncode({'source': 'RSS:2'})));
      expect(frame!.source, 'RSS:2');
      expect(frame.hasImage, isFalse);
    });

    test('ignores malformed payloads instead of failing the stream', () {
      expect(decodePreviewFrame('not json'), isNull);
      expect(decodePreviewFrame('[1,2,3]'), isNull);
      expect(decodePreviewFrame(42), isNull);
      expect(decodePreviewFrame(jsonEncode({'image': 'not-base64!!'})), isNotNull);
    });
  });
}
