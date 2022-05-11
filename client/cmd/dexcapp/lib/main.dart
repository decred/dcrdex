import 'dart:io';
import 'package:flutter/material.dart';
import 'libcore/libcore.dart';
import 'appwindow.dart';
import 'routes/register.dart';

void main() {
  var err = libcore.start();
  if (err != null && err.isNotEmpty) {
    debugPrint(err); // TODO: Show error in UI.
    exit(1);
  }

  runApp(const DexcApp());
}

class DexcApp extends StatelessWidget {
  const DexcApp({Key? key}) : super(key: key);

  // This widget is the root of your application.
  @override
  Widget build(BuildContext context) {
    final Widget startPage;
    if (libcore.isInitialized() == 0) {
      startPage = const RegisterRoute();
    } else {
      // TODO: Show login route.
      startPage = const Text('dex client is already initialized');
    }

    return AppWindow(homeRoute: startPage);
  }
}
