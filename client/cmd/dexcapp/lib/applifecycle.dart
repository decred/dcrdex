import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

class AppLifecycleManager with WidgetsBindingObserver {
  static AppLifecycleManager instance = AppLifecycleManager();

  final _windowManagerChannel =
      const MethodChannel('org.decred.dcrdex/windowManager');
  AppLifecycleObserver? _observer;

  void watch(AppLifecycleObserver observer) {
    if (_observer != null) {
      throw Exception('AppLifecycleManager.watch called repeatedly!');
    }

    _observer = observer;
    WidgetsBinding.instance!.addObserver(this);
    _windowManagerChannel.setMethodCallHandler(_windowManagerMethodCallHandler);
  }

  void stopWatching() {
    WidgetsBinding.instance!.removeObserver(this);
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    super.didChangeAppLifecycleState(state);

    if (state == AppLifecycleState.detached) {
      _observer?.didClose();
    } else {
      debugPrint('unhandled AppLifecycleState: $state');
    }
  }

  Future<void> _windowManagerMethodCallHandler(MethodCall call) async {
    String event = call.arguments['eventName'];
    switch (event) {
      case 'close':
        _observer?.didClose();
        break;

      default:
        debugPrint('unhandled method: $event');
    }
  }
}

abstract class AppLifecycleObserver {
  void didClose() {}
}
