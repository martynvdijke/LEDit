import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api_client.dart';
import '../state.dart';
import 'browser_login_screen.dart';

/// First-run (and sign-out) screen: point the app at a server and pick how to
/// authenticate. OIDC runs in an embedded browser because the IdP owns the
/// login form; API tokens and username/password cover headless servers.
///
/// The hosted instance is prefilled so the common path (log in to
/// ledit.vandijke.xyz) takes one tap, while the field stays editable for
/// self-hosted servers. A previously saved address always takes precedence.
class OnboardingScreen extends ConsumerStatefulWidget {
  const OnboardingScreen({super.key});

  @override
  ConsumerState<OnboardingScreen> createState() => _OnboardingScreenState();
}

class _OnboardingScreenState extends ConsumerState<OnboardingScreen> {
  final _formKey = GlobalKey<FormState>();
  final _serverUrl = TextEditingController(text: defaultServerUrl);
  final _credential = TextEditingController();
  final _username = TextEditingController();
  final _password = TextEditingController();

  AuthMode _mode = AuthMode.oidc;
  bool _busy = false;
  bool _serverEdited = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _restoreSavedServerUrl();
  }

  /// Prefills the address the user connected to before, if storage has one.
  ///
  /// A saved URL wins over [defaultServerUrl], but never over text the user
  /// has already typed on this screen.
  Future<void> _restoreSavedServerUrl() async {
    try {
      final saved = await ref.read(savedServerUrlProvider.future);
      if (!mounted || _serverEdited || saved == null || saved.isEmpty) return;
      setState(() {
        _serverUrl.text = saved;
        _serverUrl.selection = TextSelection.collapsed(offset: saved.length);
      });
    } catch (_) {
      // Unreadable storage falls back to the default instance.
    }
  }

  @override
  void dispose() {
    _serverUrl.dispose();
    _credential.dispose();
    _username.dispose();
    _password.dispose();
    super.dispose();
  }

  Future<void> _connect() async {
    if (!(_formKey.currentState?.validate() ?? false)) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final serverUrl = normaliseServerUrl(_serverUrl.text);
      final reachable = await LeditApi.health(serverUrl);
      if (!reachable) {
        throw ApiException('Could not reach a LEDit server at $serverUrl.');
      }

      SessionConfig? config;
      if (_mode == AuthMode.oidc) {
        if (!await LeditApi.oidcEnabled(serverUrl)) {
          throw ApiException(
            'This server does not have OIDC enabled. '
            'Use an API token or username & password instead.',
          );
        }
        if (!mounted) return;
        final cookie = await BrowserLoginScreen.show(context, serverUrl);
        if (cookie == null) return; // The user closed the login page.
        config = SessionConfig(
          serverUrl: serverUrl,
          mode: AuthMode.oidcCookie,
          sessionCookie: cookie,
        );
      } else if (_mode == AuthMode.password) {
        final cookie = await LeditApi.login(
          serverUrl,
          _username.text.trim(),
          _password.text,
        );
        config = SessionConfig(
          serverUrl: serverUrl,
          mode: AuthMode.oidcCookie,
          sessionCookie: cookie,
          username: _username.text.trim(),
        );
      } else {
        config = SessionConfig(
          serverUrl: serverUrl,
          mode: _mode,
          apiToken: _mode == AuthMode.apiToken ? _credential.text.trim() : null,
          sessionCookie: _mode == AuthMode.oidcCookie
              ? _credential.text.trim()
              : null,
        );
      }

      // Verify the credential against an authenticated read before persisting.
      final api = LeditApi(
        baseUrl: config.baseUrl,
        headers: config.authHeaders,
      );
      await api.devices();
      await ref.read(sessionControllerProvider.notifier).save(config);
    } on ApiException catch (e) {
      setState(() => _error = e.message);
    } catch (e) {
      setState(() => _error = 'Connection failed: $e');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: const Text('Connect to LEDit')),
      body: SafeArea(
        child: Form(
          key: _formKey,
          child: ListView(
            padding: const EdgeInsets.all(20),
            children: [
              Center(
                child: Image.asset(
                  'assets/brand/ledit-mark.png',
                  width: 84,
                  height: 84,
                  filterQuality: FilterQuality.medium,
                ),
              ),
              const SizedBox(height: 16),
              Text('LED matrix remote', style: theme.textTheme.headlineSmall),
              const SizedBox(height: 8),
              Text(
                'Control your matrices, switch playlists and scenes, and push '
                'priority messages from your phone.',
                style: theme.textTheme.bodyMedium,
              ),
              const SizedBox(height: 24),
              TextFormField(
                controller: _serverUrl,
                keyboardType: TextInputType.url,
                autocorrect: false,
                onChanged: (_) => _serverEdited = true,
                decoration: const InputDecoration(
                  labelText: 'Server URL',
                  hintText: 'ledit.vandijke.xyz',
                  helperText:
                      'Use the default instance below, or enter another '
                      'self-hosted LEDit server.',
                  helperMaxLines: 2,
                  border: OutlineInputBorder(),
                ),
                validator: (value) => isValidServerUrl(value ?? '')
                    ? null
                    : 'Enter the LEDit server address',
              ),
              const SizedBox(height: 16),
              DropdownButtonFormField<AuthMode>(
                initialValue: _mode,
                decoration: const InputDecoration(
                  labelText: 'Authentication',
                  border: OutlineInputBorder(),
                ),
                items: [
                  for (final mode in AuthMode.values)
                    DropdownMenuItem(value: mode, child: Text(mode.label)),
                ],
                onChanged: (mode) {
                  if (mode == null) return;
                  setState(() => _mode = mode);
                },
              ),
              const SizedBox(height: 16),
              if (_mode == AuthMode.oidc) ...[
                Card(
                  child: Padding(
                    padding: const EdgeInsets.all(12),
                    child: Row(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Icon(
                          Icons.travel_explore,
                          color: theme.colorScheme.primary,
                        ),
                        const SizedBox(width: 12),
                        const Expanded(
                          child: Text(
                            'Sign-in happens on your server\'s own login page. '
                            'Authelia (or the configured OIDC provider) handles '
                            'your credentials; the app only keeps the session '
                            'cookie it issues.',
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ] else if (_mode == AuthMode.password) ...[
                TextFormField(
                  controller: _username,
                  autocorrect: false,
                  decoration: const InputDecoration(
                    labelText: 'Username',
                    border: OutlineInputBorder(),
                  ),
                  validator: (value) => (value == null || value.trim().isEmpty)
                      ? 'Enter your username'
                      : null,
                ),
                const SizedBox(height: 16),
                TextFormField(
                  controller: _password,
                  obscureText: true,
                  decoration: const InputDecoration(
                    labelText: 'Password',
                    border: OutlineInputBorder(),
                  ),
                  validator: (value) => (value == null || value.isEmpty)
                      ? 'Enter your password'
                      : null,
                ),
              ] else
                TextFormField(
                  controller: _credential,
                  obscureText: _mode == AuthMode.apiToken,
                  autocorrect: false,
                  maxLines: _mode == AuthMode.oidcCookie ? 3 : 1,
                  decoration: InputDecoration(
                    labelText: _mode == AuthMode.apiToken
                        ? 'API token'
                        : 'Session cookie value',
                    helperText: _mode == AuthMode.apiToken
                        ? 'Create one under Admin → API tokens.'
                        : 'Sign in to LEDit in a browser, then copy the value '
                              'of the "session" cookie.',
                    border: const OutlineInputBorder(),
                  ),
                  validator: (value) =>
                      _mode != AuthMode.password &&
                          (value == null || value.trim().isEmpty)
                      ? 'Required'
                      : null,
                ),
              const SizedBox(height: 24),
              if (_error != null) ...[
                Card(
                  color: theme.colorScheme.errorContainer,
                  child: Padding(
                    padding: const EdgeInsets.all(12),
                    child: Text(
                      _error!,
                      style: TextStyle(
                        color: theme.colorScheme.onErrorContainer,
                      ),
                    ),
                  ),
                ),
                const SizedBox(height: 16),
              ],
              FilledButton.icon(
                onPressed: _busy ? null : _connect,
                icon: _busy
                    ? const SizedBox(
                        width: 16,
                        height: 16,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Icon(Icons.link),
                label: Text(
                  _busy
                      ? 'Connecting…'
                      : _mode == AuthMode.oidc
                      ? 'Sign in with browser'
                      : 'Connect',
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
