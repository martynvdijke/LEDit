import 'package:flutter/material.dart';
import 'package:webview_flutter/webview_flutter.dart';

import '../api_client.dart';

/// Browser-based login against a LEDit server.
///
/// The OIDC dance (discovery, Authelia's own login form, callback) is a
/// browser redirect flow, so the app hands it to an embedded browser and only
/// takes the `session` cookie the server issues once the callback succeeds.
/// Pops with the cookie value, or `null` when the user backs out.
class BrowserLoginScreen extends StatefulWidget {
  const BrowserLoginScreen({super.key, required this.serverUrl});

  /// Normalised LEDit base URL the session cookie belongs to.
  final String serverUrl;

  /// Pushes the screen and resolves with the captured session cookie.
  static Future<String?> show(BuildContext context, String serverUrl) {
    return Navigator.of(context).push<String>(
      MaterialPageRoute(
        builder: (_) => BrowserLoginScreen(serverUrl: serverUrl),
      ),
    );
  }

  @override
  State<BrowserLoginScreen> createState() => _BrowserLoginScreenState();
}

class _BrowserLoginScreenState extends State<BrowserLoginScreen> {
  late final WebViewController _controller;
  final WebViewCookieManager _cookies = WebViewCookieManager();

  bool _loading = true;
  bool _finished = false;
  bool _capturing = false;
  String? _error;

  String get _baseUrl => normaliseServerUrl(widget.serverUrl);

  Uri get _server => Uri.parse(_baseUrl);

  /// Cookie lookups need an absolute path; `http://host` alone has none.
  Uri get _cookieDomain => _server.replace(path: '/');

  @override
  void initState() {
    super.initState();
    _controller = WebViewController()
      ..setJavaScriptMode(JavaScriptMode.unrestricted)
      ..setNavigationDelegate(
        NavigationDelegate(
          onPageStarted: (_) {
            if (!mounted) return;
            setState(() {
              _loading = true;
              _error = null;
            });
          },
          onPageFinished: (url) {
            if (mounted) setState(() => _loading = false);
            _captureSession(url);
          },
          onUrlChange: (change) {
            final url = change.url;
            if (url != null) _captureSession(url);
          },
          onWebResourceError: (error) {
            if (error.isForMainFrame != true || !mounted) return;
            setState(() {
              _loading = false;
              _error = error.description;
            });
          },
        ),
      );
    _start();
  }

  Future<void> _start() async {
    // A session cookie left over from a previous sign-in would complete the
    // flow before the IdP is even shown.
    await _cookies.clearCookies();
    if (!mounted) return;
    await _controller.loadRequest(Uri.parse('$_baseUrl/api/auth/oidc/login'));
  }

  /// Looks for the session cookie once navigation is back on the LEDit origin.
  ///
  /// The cookie is written while handling the redirect from the OIDC callback,
  /// so retry briefly in case the WebView's cookie jar is updated just after
  /// the page callback fires.
  Future<void> _captureSession(String url) async {
    if (_finished || _capturing) return;
    final uri = Uri.tryParse(url);
    if (uri == null || uri.host != _server.host || uri.port != _server.port) {
      return;
    }
    _capturing = true;
    for (var attempt = 0; attempt < 8; attempt++) {
      final cookies = await _cookies.getCookies(domain: _cookieDomain);
      for (final cookie in cookies) {
        if (cookie.name == 'session' && cookie.value.isNotEmpty) {
          _finished = true;
          if (mounted) Navigator.of(context).pop(cookie.value);
          return;
        }
      }
      await Future<void>.delayed(const Duration(milliseconds: 150));
    }
    _capturing = false;
  }

  Future<void> _handleBack() async {
    if (await _controller.canGoBack()) {
      await _controller.goBack();
      return;
    }
    if (mounted) Navigator.of(context).pop();
  }

  @override
  Widget build(BuildContext context) {
    return PopScope(
      canPop: false,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) _handleBack();
      },
      child: Scaffold(
        appBar: AppBar(
          title: Text('Sign in to ${_server.host}'),
          actions: [
            IconButton(
              tooltip: 'Reload',
              onPressed: _finished ? null : () => _controller.reload(),
              icon: const Icon(Icons.refresh),
            ),
          ],
          bottom: _loading
              ? const PreferredSize(
                  preferredSize: Size.fromHeight(2),
                  child: LinearProgressIndicator(minHeight: 2),
                )
              : null,
        ),
        body: Column(
          children: [
            if (_error != null)
              MaterialBanner(
                content: Text('Login page failed to load: $_error'),
                leading: const Icon(Icons.error_outline),
                actions: [
                  TextButton(
                    onPressed: () => _controller.reload(),
                    child: const Text('Retry'),
                  ),
                ],
              ),
            Expanded(child: WebViewWidget(controller: _controller)),
          ],
        ),
      ),
    );
  }
}
