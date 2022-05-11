import 'package:flutter/material.dart';
import 'applifecycle.dart';
import 'libcore/libcore.dart';

class AppWindow extends StatefulWidget {
  final Widget homeRoute;

  const AppWindow({Key? key, required this.homeRoute}) : super(key: key);

  @override
  _AppWindowState createState() => _AppWindowState();
}

class _AppWindowState extends State<AppWindow> with AppLifecycleObserver {
  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'DCRDEX',
      theme: ThemeData(primarySwatch: Colors.blue),
      home: widget.homeRoute,
      debugShowCheckedModeBanner: false,
    );
  }

  @override
  void initState() {
    super.initState();
    AppLifecycleManager.instance.watch(this);
  }

  @override
  void dispose() {
    AppLifecycleManager.instance.stopWatching();
    super.dispose();
  }

  @override
  void didClose() {
    var err = libcore.stop();
    if (err != null && err.isNotEmpty) {
      debugPrint(err);
    } else {
      debugPrint("app closed; core stopped");
    }
  }
}
