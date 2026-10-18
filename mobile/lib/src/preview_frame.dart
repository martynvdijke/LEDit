import 'dart:convert';

import 'models.dart';

/// Decodes one message from the device preview WebSocket.
///
/// Frames look like `{"format":"PNG","image":"<base64>","source":"CLOCK:1"}`
/// while notification/interrupt frames are text-only:
/// `{"format":"PNG","source":"NOTIFICATION","title":"..","message":".."}`.
/// Returns null for anything that is not a JSON object so a malformed frame
/// never tears the stream down.
PreviewFrame? decodePreviewFrame(Object? message) {
  String raw;
  if (message is String) {
    raw = message;
  } else if (message is List<int>) {
    raw = utf8.decode(message, allowMalformed: true);
  } else {
    return null;
  }
  Object? decoded;
  try {
    decoded = jsonDecode(raw);
  } catch (_) {
    return null;
  }
  if (decoded is! Map) return null;

  final image = decoded['image'];
  List<int>? png;
  if (image is String && image.isNotEmpty) {
    try {
      png = base64Decode(image);
    } catch (_) {
      png = null;
    }
  }
  return PreviewFrame(
    png: png,
    source: (decoded['source'] as String?) ?? '',
    notificationTitle: decoded['title'] as String?,
    notificationText: decoded['message'] as String?,
  );
}
