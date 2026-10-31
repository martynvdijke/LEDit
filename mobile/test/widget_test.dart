import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:ledit/src/screens/onboarding_screen.dart';
import 'package:ledit/src/api_client.dart';
import 'package:ledit/src/state.dart';
import 'package:ledit/src/widgets/matrix_preview.dart';

void main() {
  testWidgets('onboarding prefills the default instance and offers OIDC', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          // First run: nothing saved yet.
          savedServerUrlProvider.overrideWith((ref) async => null),
        ],
        child: const MaterialApp(home: OnboardingScreen()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Connect to LEDit'), findsOneWidget);
    expect(find.text('Server URL'), findsOneWidget);
    expect(find.text('Authentication'), findsOneWidget);
    expect(
      find.textContaining('Use the default instance below'),
      findsOneWidget,
    );
    // The hosted instance is prefilled so logging in is the primary path.
    final serverField = tester.widget<EditableText>(find.byType(EditableText).first);
    expect(serverField.controller.text, defaultServerUrl);
    // OIDC is preselected and explains that the server owns the login form.
    expect(find.text('Single sign-on (OIDC)'), findsOneWidget);
    await tester.drag(find.byType(ListView), const Offset(0, -400));
    await tester.pumpAndSettle();
    expect(find.textContaining('Sign-in happens on your server'), findsOneWidget);
    expect(find.widgetWithText(FilledButton, 'Sign in with browser'), findsOneWidget);
    expect(find.text('API token'), findsNothing);
  });

  testWidgets('onboarding honours a saved server URL over the default', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          savedServerUrlProvider.overrideWith(
            (ref) async => 'https://matrix.example.com',
          ),
        ],
        child: const MaterialApp(home: OnboardingScreen()),
      ),
    );
    await tester.pumpAndSettle();

    final serverField = tester.widget<EditableText>(find.byType(EditableText).first);
    expect(serverField.controller.text, 'https://matrix.example.com');
    expect(serverField.controller.text, isNot(defaultServerUrl));
  });

  testWidgets('onboarding switches to username/password fields', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          savedServerUrlProvider.overrideWith((ref) async => null),
        ],
        child: const MaterialApp(home: OnboardingScreen()),
      ),
    );

    await tester.tap(find.byType(DropdownButtonFormField<AuthMode>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Username & password').last);
    await tester.pumpAndSettle();

    expect(find.text('Username'), findsOneWidget);
    expect(find.text('Password'), findsOneWidget);
    expect(find.text('API token'), findsNothing);
  });

  testWidgets('matrix preview shows a placeholder until a frame arrives', (tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: MatrixPreview(width: 64, height: 32, status: 'Offline'),
        ),
      ),
    );

    expect(find.text('Offline'), findsOneWidget);
    final aspect = tester.widget<AspectRatio>(find.byType(AspectRatio));
    expect(aspect.aspectRatio, 2.0);
  });

  testWidgets('matrix preview renders decoded frame bytes', (tester) async {
    // 1x1 PNG.
    const png = <int>[
      137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82, 0, 0, 0, 1,
      0, 0, 0, 1, 8, 6, 0, 0, 0, 31, 21, 196, 137, 0, 0, 0, 10, 73, 68, 65, 84,
      120, 156, 99, 0, 1, 0, 0, 5, 0, 1, 13, 10, 45, 180, 0, 0, 0, 0, 73, 69,
      78, 68, 174, 66, 96, 130,
    ];
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(body: MatrixPreview(width: 64, height: 32, png: png)),
      ),
    );

    expect(find.byType(Image), findsOneWidget);
    expect(find.text('Waiting for frame…'), findsNothing);
  });
}
