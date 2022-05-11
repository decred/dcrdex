import 'dart:ffi';
import 'dart:isolate';
import 'dart:io';
import "package:ffi/ffi.dart";
import 'package:flutter/foundation.dart';
import 'libcore_bindings.dart';

final libcore = LibCore();

class LibCore extends GoCore {
  static const network = "testnet";

  LibCore._(DynamicLibrary dynamicLibrary) : super(dynamicLibrary);

  factory LibCore() {
    final DynamicLibrary dLib;
    if (Platform.isMacOS) {
      dLib = DynamicLibrary.open('libcore.dylib');
    } else {
      throw Exception('$Platform.operatingSystem is not yet supported.');
    }
    return LibCore._(dLib);
  }

  String? start() {
    final dataDir = "".toCString(); // leave empty to use default dexc dataDir.
    var err = startCore(dataDir, network.toCString()).toDartString();
    if (err.isNotEmpty) {
      return err;
    }

    _runCoreInBackground()
        .then((value) => debugPrint("core has stopped running in background"));

    waitTillReady();
    return null;
  }

  String? stop() {
    return kill().toDartString();
  }
}

// This should be a method in the LibCore class but starting an isolate
// from a class method captures all the class variables in the isolate
// scope, including variables that are not permitted to be in an isolate.
// See https://github.com/dart-lang/sdk/issues/48566.
Future<void> _runCoreInBackground() async {
  Function runCore(SendPort? port) {
    libcore.run();
    Isolate.exit(port);
  }

  final p = ReceivePort();
  await Isolate.spawn(runCore, p.sendPort);
  return await p.first;
}

extension on Pointer<Int8> {
  String toDartString() {
    return cast<Utf8>().toDartString();
  }
}

extension on String {
  Pointer<Int8> toCString() {
    return toNativeUtf8().cast<Int8>();
  }
}
