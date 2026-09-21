import 'package:firebase_core/firebase_core.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/foundation.dart';

/// IMPORTANT (robert): this will NOT do anything yet, and is written to
/// fail safely rather than crash the app, because Firebase isn't
/// configured for this project — there's no firebase_options.dart, no
/// google-services.json/GoogleService-Info.plist. Before this actually
/// registers a device:
///   1. Create a Firebase project (or reuse one if you already have one
///      for another product) and run `flutterfire configure` from this
///      project's root — that generates firebase_options.dart and the
///      platform config files, and is a one-time setup step per app.
///   2. Call `Firebase.initializeApp(options: DefaultFirebaseOptions.currentPlatform)`
///      below instead of the bare `Firebase.initializeApp()`.
///   3. Add the Gateway call noted in the TODO below so the FCM token
///      actually reaches Notification.RegisterDevice — right now it only
///      prints to the debug console.
class PushNotifications {
  static Future<void> initialize() async {
    try {
      await Firebase.initializeApp();
    } catch (e) {
      debugPrint('Push notifications skipped — Firebase not configured: $e');
      return;
    }

    final messaging = FirebaseMessaging.instance;
    await messaging.requestPermission();
    final token = await messaging.getToken();
    if (token != null) {
      // TODO(robert): call Notification.RegisterDevice through the Gateway
      // with this FCM token + the signed-in rider's identity_id, and again
      // whenever FirebaseMessaging.instance.onTokenRefresh fires.
      debugPrint('FCM token (not yet sent to backend): $token');
    }

    FirebaseMessaging.onMessage.listen((message) {
      // TODO(robert): FCM shows the notification automatically while the
      // app is backgrounded; this callback only fires in the foreground,
      // where you likely want an in-app banner/snackbar instead of
      // nothing — the system won't show one for you here.
      debugPrint('Push received in foreground: ${message.notification?.title}');
    });
  }
}
