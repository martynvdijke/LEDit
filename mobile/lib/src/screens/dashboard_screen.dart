import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api_client.dart';
import '../models.dart';
import '../state.dart';
import '../widgets/matrix_preview.dart';
import 'device_screen.dart';

/// Fleet overview: every device with its liveness, live preview and the
/// wall-wide transport controls.
class DashboardScreen extends ConsumerWidget {
  const DashboardScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final session = ref.watch(sessionControllerProvider).value;
    final devices = ref.watch(devicesProvider);
    final feed = ref.watch(feedStatusProvider);

    return Scaffold(
      appBar: AppBar(
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('LEDit'),
            if (session != null)
              Text(
                Uri.tryParse(session.baseUrl)?.host ?? session.baseUrl,
                style: Theme.of(context).textTheme.labelSmall,
              ),
          ],
        ),
        actions: [
          IconButton(
            tooltip: 'Refresh',
            onPressed: () => refreshServerState(ref),
            icon: const Icon(Icons.refresh),
          ),
          IconButton(
            tooltip: 'Sign out',
            onPressed: () => ref.read(sessionControllerProvider.notifier).signOut(),
            icon: const Icon(Icons.logout),
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: () => refreshServerState(ref),
        child: devices.when(
          loading: () => const Center(child: CircularProgressIndicator()),
          error: (error, _) => _ErrorView(
            message: error.toString(),
            onRetry: () => ref.invalidate(devicesProvider),
          ),
          data: (list) {
            if (list.isEmpty) {
              return const _ErrorView(
                message: 'No devices are configured on this server yet.',
              );
            }
            return ListView(
              padding: const EdgeInsets.all(12),
              children: [
                _WallControls(status: feed.value),
                const SizedBox(height: 8),
                for (final device in list) ...[
                  _DeviceCard(device: device),
                  const SizedBox(height: 12),
                ],
              ],
            );
          },
        ),
      ),
    );
  }
}

class _ErrorView extends StatelessWidget {
  const _ErrorView({required this.message, this.onRetry});

  final String message;
  final VoidCallback? onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Text(message, textAlign: TextAlign.center),
            if (onRetry != null) ...[
              const SizedBox(height: 12),
              FilledButton(onPressed: onRetry, child: const Text('Retry')),
            ],
          ],
        ),
      ),
    );
  }
}

/// Wall-wide feed transport: pause/resume and skip to the next source.
class _WallControls extends ConsumerWidget {
  const _WallControls({this.status});

  final FeedStatus? status;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final api = ref.watch(apiProvider);
    final paused = status?.paused ?? false;
    Future<void> run(Future<void> Function(LeditApi api) action) async {
      if (api == null) return;
      try {
        await action(api);
        await refreshServerState(ref);
      } on ApiException catch (e) {
        if (context.mounted) {
          ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(e.message)));
        }
      }
    }

    return Card(
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('Wall feed', style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 4),
            Text(
              'Now: ${status?.current ?? '—'}  →  Next: ${status?.next ?? '—'}',
              style: Theme.of(context).textTheme.bodySmall,
            ),
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              children: [
                OutlinedButton.icon(
                  onPressed: () => run((a) => paused ? a.feedResume() : a.feedPause()),
                  icon: Icon(paused ? Icons.play_arrow : Icons.pause),
                  label: Text(paused ? 'Resume' : 'Pause'),
                ),
                OutlinedButton.icon(
                  onPressed: () => run((a) => a.feedNext()),
                  icon: const Icon(Icons.skip_next),
                  label: const Text('Next'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _DeviceCard extends ConsumerWidget {
  const _DeviceCard({required this.device});

  final Device device;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final frame = ref.watch(devicePreviewProvider(device.id));
    final notification = frame.value?.isNotification == true
        ? frame.value?.notificationText
        : null;

    return Card(
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: () => Navigator.of(context).push(
          MaterialPageRoute<void>(
            builder: (_) => DeviceScreen(device: device),
          ),
        ),
        child: Padding(
          padding: const EdgeInsets.all(12),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Expanded(
                    child: Text(
                      device.name,
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                  ),
                  _LivenessChip(device: device),
                ],
              ),
              const SizedBox(height: 4),
              Text(
                '${device.width}×${device.height} · ${_bindingLabel(device)}',
                style: Theme.of(context).textTheme.bodySmall,
              ),
              const SizedBox(height: 8),
              MatrixPreview(
                width: device.width,
                height: device.height,
                png: frame.value?.png,
                notification: notification,
                status: device.online ? 'Waiting for frame…' : 'Offline',
              ),
            ],
          ),
        ),
      ),
    );
  }

  static String _bindingLabel(Device device) {
    if (device.contentMode == 'playlist' && device.playlistName.isNotEmpty) {
      return 'Playlist: ${device.playlistName}';
    }
    if (device.contentMode == 'scheduled') {
      return 'Scheduled playlist';
    }
    return device.contentMode.isEmpty ? 'Rotation' : device.contentMode;
  }
}

class _LivenessChip extends StatelessWidget {
  const _LivenessChip({required this.device});

  final Device device;

  @override
  Widget build(BuildContext context) {
    final online = device.online;
    final color = online ? Colors.green : Theme.of(context).colorScheme.outline;
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Container(
          width: 10,
          height: 10,
          decoration: BoxDecoration(color: color, shape: BoxShape.circle),
        ),
        const SizedBox(width: 6),
        Text(online ? 'Online' : device.liveness),
      ],
    );
  }
}
