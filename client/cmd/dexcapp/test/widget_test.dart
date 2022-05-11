// This is a basic Flutter widget test.
//
// To perform an interaction with a widget in your test, use the WidgetTester
// utility that Flutter provides. For example, you can send tap and scroll
// gestures. You can also use WidgetTester to find child widgets in the widget
// tree, read text, and verify that the values of widget properties are correct.

import 'package:flutter_test/flutter_test.dart';

import 'package:dcrdex/main.dart';

void main() {
// TODO: Fix tests.
  testWidgets('App', (WidgetTester tester) async {
    // Build our app and trigger a frame.
    await tester.pumpWidget(const DexcApp());

    // Verify that the app starts with the Register page.
    expect(find.text('Set App Password'), findsOneWidget);
  });
}
