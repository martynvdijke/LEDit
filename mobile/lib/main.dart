import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'src/screens/dashboard_screen.dart';
import 'src/screens/onboarding_screen.dart';
import 'src/state.dart';

void main() {
  runApp(const ProviderScope(child: LeditApp()));
}

class LeditApp extends ConsumerWidget {
  const LeditApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final session = ref.watch(sessionControllerProvider);
    return MaterialApp(
      title: 'LEDit',
      debugShowCheckedModeBanner: false,
      theme: ThemeData(
        colorScheme: ColorScheme.fromSeed(
          seedColor: const Color(0xFF00B4D8),
          brightness: Brightness.dark,
        ),
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
    );
  }
}
