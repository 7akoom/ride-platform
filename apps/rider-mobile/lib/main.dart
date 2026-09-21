import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'core/navigation.dart';
import 'features/onboarding/splash_screen.dart';
import 'state/app_prefs.dart';
import 'state/locale_provider.dart';
import 'state/push_notifications.dart';
import 'state/theme_provider.dart';
import 'theme/app_theme.dart';

void main() async {
  WidgetsFlutterBinding.ensureInitialized();
  await PushNotifications.initialize();

  final savedLocale = await AppPrefs.readLocale();
  final savedThemeMode = await AppPrefs.readThemeMode();

  runApp(
    ProviderScope(
      overrides: [
        localeProvider.overrideWith((ref) => savedLocale),
        themeModeProvider.overrideWith((ref) => savedThemeMode),
      ],
      child: const RiderApp(),
    ),
  );
}

class RiderApp extends ConsumerWidget {
  const RiderApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final locale = ref.watch(localeProvider);
    final themeMode = ref.watch(themeModeProvider);
    return MaterialApp(
      navigatorKey: rootNavigatorKey,
      title: 'Ride Platform',
      debugShowCheckedModeBanner: false,
      locale: locale,
      supportedLocales: const [Locale('ar'), Locale('en')],
      localizationsDelegates: const [
        GlobalMaterialLocalizations.delegate,
        GlobalWidgetsLocalizations.delegate,
        GlobalCupertinoLocalizations.delegate,
      ],
      theme: AppTheme.light(),
      darkTheme: AppTheme.dark(),
      themeMode: themeMode,
      home: const SplashScreen(),
    );
  }
}
