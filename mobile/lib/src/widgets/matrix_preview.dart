import 'dart:typed_data';

import 'package:flutter/material.dart';

/// Renders a matrix frame at its true pixel ratio with no smoothing, so the
/// LEDs stay crisp and the aspect ratio matches the physical panel.
class MatrixPreview extends StatelessWidget {
  const MatrixPreview({
    super.key,
    required this.width,
    required this.height,
    this.png,
    this.notification,
    this.status = '',
  });

  final int width;
  final int height;
  final List<int>? png;

  /// Notification text currently held on the matrix, if any.
  final String? notification;

  /// Fallback label (e.g. "waiting for frame").
  final String status;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final rows = height > 0 ? height : 32;
    final cols = width > 0 ? width : 64;
    return AspectRatio(
      aspectRatio: cols / rows,
      child: DecoratedBox(
        decoration: BoxDecoration(
          color: Colors.black,
          borderRadius: BorderRadius.circular(8),
          border: Border.all(color: theme.colorScheme.outlineVariant),
        ),
        child: ClipRRect(
          borderRadius: BorderRadius.circular(7),
          child: Stack(
            fit: StackFit.expand,
            children: [
              if (png != null && png!.isNotEmpty)
                Image.memory(
                  Uint8List.fromList(png!),
                  fit: BoxFit.fill,
                  filterQuality: FilterQuality.none,
                  gaplessPlayback: true,
                  semanticLabel: 'Live matrix frame',
                )
              else
                Center(
                  child: Text(
                    status.isEmpty ? 'Waiting for frame…' : status,
                    style: theme.textTheme.bodySmall?.copyWith(color: Colors.white54),
                    textAlign: TextAlign.center,
                  ),
                ),
              if (notification != null && notification!.isNotEmpty)
                Align(
                  alignment: Alignment.bottomCenter,
                  child: Container(
                    width: double.infinity,
                    color: Colors.black.withValues(alpha: 0.65),
                    padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                    child: Text(
                      notification!,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.labelMedium?.copyWith(color: Colors.white),
                    ),
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
}
