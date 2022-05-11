import Cocoa
import FlutterMacOS

class MainFlutterWindow: NSWindow, NSWindowDelegate {
  public var emitEvent: ((String) -> Void)?

  override func awakeFromNib() {
    let flutterViewController = FlutterViewController.init()
    let windowFrame = self.frame
    self.contentViewController = flutterViewController
    self.setFrame(windowFrame, display: true)
    
    // Open a channel to use for sending window events from the
    // NSWindowDelegate to the flutter app.
    let channel = FlutterMethodChannel(name: "org.decred.dcrdex/windowManager", binaryMessenger: flutterViewController.engine.binaryMessenger)
    emitEvent = { (eventName: String) in
        channel.invokeMethod("onEvent", arguments: ["eventName": eventName], result: nil)
    }
    self.delegate = self

    RegisterGeneratedPlugins(registry: flutterViewController)

    super.awakeFromNib()
  }

  public func windowShouldClose(_ sender: NSWindow) -> Bool {
    emitEvent?("close")
    return true;
  }
}
