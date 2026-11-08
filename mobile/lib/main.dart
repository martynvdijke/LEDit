import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'src/api_client.dart';
import 'src/screens/dashboard_screen.dart';
import 'src/screens/onboarding_screen.dart';
import 'src/state.dart';
import 'src/telemetry/telemetry.dart';

void main() {
  runApp(const ProviderScope(child: LeditApp()));
}

class LeditApp extends ConsumerWidget {
  const LeditApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final session = ref.watch(sessionControllerProvider);
    return TelemetryBootstrap(
      child: MaterialApp(
        title: 'LEDit',
        debugShowCheckedModeBanner: false,
        theme: ThemeData(
          colorScheme: ColorScheme.fromSeed(
            seedColor: const Color(0xFFB7FF35),
            brightness: Brightness.dark,
          ),
          scaffoldBackgroundColor: const Color(0xFF050806),
          useMaterial3: true,
        ),
        home: session.when(
          loading: () => const Scaffold(
            body: Center(child: CircularProgressIndicator()),
          ),
          error: (error, _) => Scaffold(
            body: Center(child: Text('Could not read stored settings: $error')),
          ),
          data: (config) =>
              config == null ? const OnboardingScreen() : const DashboardScreen(),
        ),
      ),
    );
  }
}

/// Starts telemetry once the saved server is known.
///
/// This never blocks the first frame and never throws: the server URL lookup
/// and the settings fetch both run detached, so an offline device simply
/// exports nothing.
class TelemetryBootstrap extends ConsumerStatefulWidget {
  const TelemetryBootstrap({super.key, required this.child});

  final Widget child;

  @override
  ConsumerState<TelemetryBootstrap> createState() => _TelemetryBootstrapState();
}

class _TelemetryBootstrapState extends ConsumerState<TelemetryBootstrap> {
  String? _startedFor;

  @override
  void initState() {
    super.initState();
    unawaited(_startTelemetry());
  }

  Future<void> _startTelemetry() async {
    try {
      String? saved;
      try {
        saved = await ref.read(savedServerUrlProvider.future);
      } catch (_) {
        saved = null;
      }
      final url = normaliseServerUrl(saved ?? defaultServerUrl);
      if (url.isEmpty || _startedFor == url) return;
      _startedFor = url;
      await Telemetry.instance.startFromServer(url);
    } catch (_) {
      // Telemetry must never affect the app.
    }
  }

  @override
  Widget build(BuildContext context) => widget.child;
}
