import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:url_launcher/url_launcher.dart';

import '../api_client.dart';
import '../state.dart';

/// Account and legal settings for the signed-in remote.
///
/// The Android app has no local account database: everything shown here comes
/// from the server, and deletion goes through the same `DELETE /api/v1/me`
/// endpoint as the web page.
class SettingsScreen extends ConsumerStatefulWidget {
  const SettingsScreen({super.key});

  static Future<void> open(BuildContext context) {
    return Navigator.of(context).push<void>(
      MaterialPageRoute(builder: (_) => const SettingsScreen()),
    );
  }

  @override
  ConsumerState<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends ConsumerState<SettingsScreen> {
  String _username = '';
  String? _error;
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final api = ref.read(apiProvider);
    final session = ref.read(sessionControllerProvider).value;
    final fallback = session?.username ?? '';
    if (api == null) {
      setState(() {
        _username = fallback;
        _loading = false;
      });
      return;
    }
    try {
      final profile = await api.me();
      if (!mounted) return;
      setState(() {
        _username = (profile['username'] as String?) ?? fallback;
        _loading = false;
      });
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() {
        _username = fallback;
        _error = e.message;
        _loading = false;
      });
    }
  }

  Future<void> _openLegal(String path) async {
    final session = ref.read(sessionControllerProvider).value;
    if (session == null) return;
    final uri = Uri.parse('${session.baseUrl}$path');
    final launched = await launchUrl(uri, mode: LaunchMode.externalApplication);
    if (!launched && mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('Could not open $uri')),
      );
    }
  }

  Future<void> _deleteAccount() async {
    final api = ref.read(apiProvider);
    if (api == null || _username.isEmpty) return;
    final deleted = await showDialog<bool>(
      context: context,
      builder: (_) => _DeleteAccountDialog(api: api, username: _username),
    );
    if (deleted != true || !mounted) return;
    await ref.read(sessionControllerProvider.notifier).signOut();
    if (!mounted) return;
    Navigator.of(context).popUntil((route) => route.isFirst);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final session = ref.watch(sessionControllerProvider).value;
    return Scaffold(
      appBar: AppBar(title: const Text('Settings')),
      body: SafeArea(
        child: _loading
            ? const Center(child: CircularProgressIndicator())
            : ListView(
                padding: const EdgeInsets.fromLTRB(20, 12, 20, 36),
                children: [
                  Text('Account', style: theme.textTheme.titleMedium),
                  const SizedBox(height: 8),
                  Card(
                    child: ListTile(
                      leading: const Icon(Icons.person_outline),
                      title: Text(_username.isEmpty ? 'Signed in' : _username),
                      subtitle: Text(
                        session == null
                            ? 'No active session'
                            : Uri.tryParse(session.baseUrl)?.host ??
                                session.baseUrl,
                      ),
                    ),
                  ),
                  if (_error != null) ...[
                    const SizedBox(height: 8),
                    Text(
                      _error!,
                      style: TextStyle(color: theme.colorScheme.error),
                    ),
                  ],
                  const SizedBox(height: 24),
                  Text('Legal', style: theme.textTheme.titleMedium),
                  const SizedBox(height: 8),
                  Card(
                    clipBehavior: Clip.antiAlias,
                    child: Column(
                      children: [
                        ListTile(
                          leading: const Icon(Icons.privacy_tip_outlined),
                          title: const Text('Privacy policy'),
                          trailing: const Icon(Icons.open_in_new),
                          onTap: () => _openLegal('/privacy'),
                        ),
                        const Divider(height: 1),
                        ListTile(
                          leading: const Icon(Icons.description_outlined),
                          title: const Text('Terms of use'),
                          trailing: const Icon(Icons.open_in_new),
                          onTap: () => _openLegal('/terms'),
                        ),
                        const Divider(height: 1),
                        ListTile(
                          leading: const Icon(Icons.delete_forever_outlined),
                          title: const Text('Delete account'),
                          subtitle: const Text(
                            'Erases the account and revokes its sessions and tokens.',
                          ),
                          onTap: _username.isEmpty ? null : _deleteAccount,
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(height: 24),
                  TextButton.icon(
                    onPressed: () =>
                        ref.read(sessionControllerProvider.notifier).signOut(),
                    icon: const Icon(Icons.logout),
                    label: const Text('Sign out'),
                  ),
                ],
              ),
      ),
    );
  }
}

class _DeleteAccountDialog extends StatefulWidget {
  const _DeleteAccountDialog({required this.api, required this.username});

  final LeditApi api;
  final String username;

  @override
  State<_DeleteAccountDialog> createState() => _DeleteAccountDialogState();
}

class _DeleteAccountDialogState extends State<_DeleteAccountDialog> {
  static const _effects = [
    'Your username, email address and password are erased.',
    'Your OIDC link, browser sessions and owned API tokens are removed.',
    'Devices, playlists, schedules and guest uploads are instance content and stay.',
    'This cannot be undone.',
  ];

  final _confirm = TextEditingController();
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _confirm.dispose();
    super.dispose();
  }

  bool get _matches {
    final text = _confirm.text.trim();
    return text == widget.username || text == '@${widget.username}';
  }

  Future<void> _submit() async {
    if (_busy || !_matches) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await widget.api.deleteAccount(_confirm.text.trim());
      if (!mounted) return;
      Navigator.of(context).pop(true);
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        _error = e.statusCode == 409
            ? 'This is the only administrator. Promote another admin first.'
            : e.message;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        _error = 'Delete failed: $e';
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return AlertDialog(
      title: const Text('Delete account?'),
      content: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            for (final effect in _effects)
              Padding(
                padding: const EdgeInsets.only(bottom: 6),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text('• '),
                    Expanded(child: Text(effect)),
                  ],
                ),
              ),
            const SizedBox(height: 12),
            Text(
              'Type ${widget.username} to confirm.',
              style: theme.textTheme.bodySmall,
            ),
            const SizedBox(height: 8),
            TextField(
              controller: _confirm,
              enabled: !_busy,
              autofocus: true,
              autocorrect: false,
              decoration: InputDecoration(
                labelText: 'Username',
                hintText: widget.username,
                border: const OutlineInputBorder(),
              ),
              onChanged: (_) => setState(() {}),
            ),
            if (_error != null) ...[
              const SizedBox(height: 10),
              Text(_error!, style: TextStyle(color: theme.colorScheme.error)),
            ],
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: _busy ? null : () => Navigator.of(context).pop(false),
          child: const Text('Cancel'),
        ),
        FilledButton(
          style: FilledButton.styleFrom(
            backgroundColor: theme.colorScheme.error,
            foregroundColor: theme.colorScheme.onError,
          ),
          onPressed: _busy || !_matches ? null : _submit,
          child: _busy
              ? const SizedBox(
                  width: 18,
                  height: 18,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              : const Text('Delete'),
        ),
      ],
    );
  }
}
