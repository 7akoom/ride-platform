import 'package:flutter/widgets.dart';

/// Lets code that is not a widget (the API client, when a session ends) move the app to
/// another screen.
final GlobalKey<NavigatorState> rootNavigatorKey = GlobalKey<NavigatorState>();
