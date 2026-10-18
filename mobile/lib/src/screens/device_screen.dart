import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:image_picker/image_picker.dart';

import '../api_client.dart';
import '../models.dart';
import '../state.dart';
import '../widgets/matrix_preview.dart';

/// Single-device control surface: live preview, brightness, playlist/scene
/// switching and the priority message composer.
class DeviceScreen extends ConsumerStatefulWidget {
  const DeviceScreen({super.key, required this.device});

  final Device device;

  @override
  ConsumerState<DeviceScreen> createState() => _DeviceScreenState();
}

class _DeviceScreenState extends ConsumerState<DeviceScreen> {
  /// Brightness changes are debounced so dragging the slider does not flood
  /// the server (see the change design: 150ms).
  static const _brightnessDebounce = Duration(milliseconds: 150);

  late int _brightness = widget.device.brightness;
  Timer? _debounce;

  @override
  void dispose() {
    _debounce?.cancel();
    super.dispose();
  }

  void _onBrightnessChanged(double value) {
    setState(() => _brightness = value.round());
    _debounce?.cancel();
    _debounce = Timer(_brightnessDebounce, _pushBrightness);
  }

  Future<void> _pushBrightness() async {
    final api = ref.read(apiProvider);
    if (api == null) return;
    try {
      await api.setBrightness(widget.device.id, _brightness);
    } on ApiException catch (e) {
      _snack(e.message);
    }
  }

  void _snack(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(message)));
  }

  Future<void> _select(String target, String label) async {
    final api = ref.read(apiProvider);
    if (api == null) return;
    try {
      await api.select(widget.device.id, target);
      await refreshServerState(ref);
      _snack('$label is now showing on ${widget.device.name}.');
    } on ApiException catch (e) {
      _snack(e.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    final device = ref
            .watch(devicesProvider)
            .value
            ?.where((d) => d.id == widget.device.id)
            .firstOrNull ??
        widget.device;
    final frame = ref.watch(devicePreviewProvider(device.id));
    final preview = frame.value;

    return Scaffold(
      appBar: AppBar(
        title: Text(device.name),
        actions: [
          IconButton(
            tooltip: 'Compose priority message',
            onPressed: () => _openComposer(device),
            icon: const Icon(Icons.campaign),
          ),
        ],
      ),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          MatrixPreview(
            width: device.width,
            height: device.height,
            png: preview?.png,
            notification: preview?.isNotification == true ? preview?.notificationText : null,
            status: device.online ? 'Waiting for frame…' : 'Device offline',
          ),
          const SizedBox(height: 8),
          Row(
            children: [
              _StatusDot(online: device.online, label: device.liveness),
              const Spacer(),
              Text(
                preview?.source.isNotEmpty == true ? preview!.source : 'no frame yet',
                style: Theme.of(context).textTheme.bodySmall,
              ),
            ],
          ),
          const Divider(height: 32),
          Text('Brightness', style: Theme.of(context).textTheme.titleMedium),
          Row(
            children: [
              const Icon(Icons.brightness_low),
              Expanded(
                child: Slider(
                  value: _brightness.toDouble(),
                  min: 0,
                  max: 100,
                  divisions: 100,
                  label: '$_brightness%',
                  onChanged: _onBrightnessChanged,
                  onChangeEnd: (_) => _pushBrightness(),
                ),
              ),
              const Icon(Icons.brightness_high),
              SizedBox(
                width: 44,
                child: Text('$_brightness%', textAlign: TextAlign.end),
              ),
            ],
          ),
          const Divider(height: 32),
          Text('Content', style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            children: [
              OutlinedButton.icon(
                onPressed: () => _openPlaylistPicker(device),
                icon: const Icon(Icons.queue_music),
                label: const Text('Playlist'),
              ),
              OutlinedButton.icon(
                onPressed: () => _openScenePicker(device),
                icon: const Icon(Icons.theater_comedy),
                label: const Text('Scene'),
              ),
            ],
          ),
          const Divider(height: 32),
          Text('Wall transport', style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            children: [
              OutlinedButton.icon(
                onPressed: () => _transport((api) => api.feedPause()),
                icon: const Icon(Icons.pause),
                label: const Text('Pause'),
              ),
              OutlinedButton.icon(
                onPressed: () => _transport((api) => api.feedResume()),
                icon: const Icon(Icons.play_arrow),
                label: const Text('Resume'),
              ),
              OutlinedButton.icon(
                onPressed: () => _transport((api) => api.feedNext()),
                icon: const Icon(Icons.skip_next),
                label: const Text('Next'),
              ),
            ],
          ),
        ],
      ),
    );
  }

  Future<void> _transport(Future<void> Function(LeditApi api) action) async {
    final api = ref.read(apiProvider);
    if (api == null) return;
    try {
      await action(api);
      await refreshServerState(ref);
    } on ApiException catch (e) {
      _snack(e.message);
    }
  }

  Future<void> _openPlaylistPicker(Device device) async {
    final playlists = await ref.read(playlistsProvider.future);
    if (!mounted) return;
    await _showPicker(
      title: 'Activate playlist',
      entries: [
        for (final pl in playlists.where((p) => p.enabled))
          _PickerEntry(
            label: pl.name,
            subtitle: '${pl.itemCount} item${pl.itemCount == 1 ? '' : 's'}',
            target: pl.selectTarget,
            selected: device.playlistId == pl.id,
          ),
      ],
    );
  }

  Future<void> _openScenePicker(Device device) async {
    final scenes = await ref.read(scenesProvider.future);
    if (!mounted) return;
    await _showPicker(
      title: 'Activate scene',
      entries: [
        for (final scene in scenes.where((s) => s.enabled))
          _PickerEntry(
            label: scene.name,
            subtitle: scene.ttlSeconds == null ? 'until replaced' : '${scene.ttlSeconds}s',
            target: scene.selectTarget,
            selected: scene.active,
          ),
      ],
    );
  }

  Future<void> _showPicker({
    required String title,
    required List<_PickerEntry> entries,
  }) async {
    if (entries.isEmpty) {
      _snack('Nothing to pick — configure playlists or scenes on the server.');
      return;
    }
    final chosen = await showModalBottomSheet<_PickerEntry>(
      context: context,
      showDragHandle: true,
      builder: (context) => ListView(
        shrinkWrap: true,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
            child: Text(title, style: Theme.of(context).textTheme.titleMedium),
          ),
          for (final entry in entries)
            ListTile(
              title: Text(entry.label),
              subtitle: Text(entry.subtitle),
              selected: entry.selected,
              trailing: entry.selected ? const Icon(Icons.check) : null,
              onTap: () => Navigator.of(context).pop(entry),
            ),
          const SizedBox(height: 12),
        ],
      ),
    );
    if (chosen != null) {
      await _select(chosen.target, chosen.label);
    }
  }

  Future<void> _openComposer(Device device) async {
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (context) => _ComposerSheet(device: device, onSent: _snack),
    );
  }
}

class _PickerEntry {
  const _PickerEntry({
    required this.label,
    required this.subtitle,
    required this.target,
    required this.selected,
  });

  final String label;
  final String subtitle;
  final String target;
  final bool selected;
}

class _StatusDot extends StatelessWidget {
  const _StatusDot({required this.online, required this.label});

  final bool online;
  final String label;

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(Icons.circle, size: 10, color: online ? Colors.green : Colors.grey),
        const SizedBox(width: 6),
        Text(online ? 'Online' : label),
      ],
    );
  }
}

/// Priority message composer: text or image, with a duration (TTL) picker.
///
/// Text uses the device message endpoint, which always interrupts the current
/// content (notifications outrank every other feed tier). Images are uploaded
/// into the server's image library and then selected on the device.
class _ComposerSheet extends ConsumerStatefulWidget {
  const _ComposerSheet({required this.device, required this.onSent});

  final Device device;
  final void Function(String message) onSent;

  @override
  ConsumerState<_ComposerSheet> createState() => _ComposerSheetState();
}

class _ComposerSheetState extends ConsumerState<_ComposerSheet> {
  static const _durations = <int, String>{
    5: '5s',
    15: '15s',
    30: '30s',
    60: '60s',
    300: '5m',
  };

  final _text = TextEditingController();
  int _ttl = 30;
  XFile? _image;
  bool _busy = false;

  @override
  void dispose() {
    _text.dispose();
    super.dispose();
  }

  Future<void> _pickImage() async {
    final picked = await ImagePicker().pickImage(source: ImageSource.gallery);
    if (picked != null) setState(() => _image = picked);
  }

  Future<void> _send() async {
    final api = ref.read(apiProvider);
    if (api == null) return;
    final text = _text.text.trim();
    if (text.isEmpty && _image == null) {
      widget.onSent('Enter a message or pick an image first.');
      return;
    }
    setState(() => _busy = true);
    try {
      if (text.isNotEmpty) {
        await api.sendMessage(widget.device.id, text, ttlSeconds: _ttl);
      }
      if (_image != null) {
        final uploaded = await api.uploadImage(_image!.path, fileName: _image!.name);
        if (uploaded != null) {
          await api.select(widget.device.id, 'source:image:${uploaded.id}');
        }
      }
      if (mounted) Navigator.of(context).pop();
      widget.onSent(
        _image == null
            ? 'Message sent to ${widget.device.name}.'
            : 'Image uploaded; it shows on ${widget.device.name} when it is part of the rotation.',
      );
    } on ApiException catch (e) {
      widget.onSent(e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: EdgeInsets.only(
        left: 16,
        right: 16,
        top: 8,
        bottom: MediaQuery.of(context).viewInsets.bottom + 16,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('Priority message', style: theme.textTheme.titleMedium),
          const SizedBox(height: 4),
          Text(
            'Interrupts ${widget.device.name} immediately. Notifications outrank '
            'scenes, alarms and pins, then the matrix resumes.',
            style: theme.textTheme.bodySmall,
          ),
          const SizedBox(height: 12),
          TextField(
            controller: _text,
            maxLength: 255,
            minLines: 1,
            maxLines: 3,
            decoration: const InputDecoration(
              labelText: 'Message',
              border: OutlineInputBorder(),
            ),
          ),
          const SizedBox(height: 8),
          Text('Duration', style: theme.textTheme.labelLarge),
          Wrap(
            spacing: 8,
            children: [
              for (final entry in _durations.entries)
                ChoiceChip(
                  label: Text(entry.value),
                  selected: _ttl == entry.key,
                  onSelected: (_) => setState(() => _ttl = entry.key),
                ),
            ],
          ),
          const SizedBox(height: 12),
          Row(
            children: [
              OutlinedButton.icon(
                onPressed: _busy ? null : _pickImage,
                icon: const Icon(Icons.image),
                label: Text(_image == null ? 'Attach image' : _image!.name),
              ),
              const Spacer(),
              FilledButton.icon(
                onPressed: _busy ? null : _send,
                icon: _busy
                    ? const SizedBox(
                        width: 16,
                        height: 16,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Icon(Icons.send),
                label: const Text('Send to matrix'),
              ),
            ],
          ),
        ],
      ),
    );
  }
}
