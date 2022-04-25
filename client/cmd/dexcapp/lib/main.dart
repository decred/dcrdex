import 'package:flutter/material.dart';
import 'pages/register.dart';

void main() {
  runApp(const App());
}

class App extends StatelessWidget {
  const App({Key? key}) : super(key: key);

  // This widget is the root of your application.
  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'DCRDEX',
      theme: ThemeData(
        primarySwatch: Colors.blue, // toolBar color
      ),
      home: Scaffold(
        body: Container(
          padding: const EdgeInsets.symmetric(horizontal: 24.0),
          child: const RegisterWidget(),
        ),
      ),
    );
  }
}
