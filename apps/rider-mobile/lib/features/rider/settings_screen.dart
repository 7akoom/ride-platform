import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:share_plus/share_plus.dart';

import '../../state/app_prefs.dart';
import '../../state/locale_provider.dart';
import '../../state/theme_provider.dart';
import '../../theme/app_theme.dart';

/// IMPORTANT (robert): every screen's Arabic copy is hardcoded directly in
/// the widget tree, not wired through AppLocalizations/.arb files. Picking
/// "English" here changes MaterialApp's `locale` (affects native widgets:
/// date pickers, the back-button's semantics label, number formatting) but
/// will NOT translate a single button or label on screen — that needs
/// every hardcoded string moved into .arb files per language, a real,
/// separate content-localization pass. Kurdish Sorani is further behind:
/// Flutter's GlobalMaterialLocalizations has no 'ku' delegate at all, so
/// it's shown as "قريباً" instead of wired to anything.
class SettingsScreen extends ConsumerWidget {
  const SettingsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;
    final locale = ref.watch(localeProvider);
    final themeMode = ref.watch(themeModeProvider);

    return Scaffold(
      backgroundColor: colors.surface100,
      appBar: AppBar(backgroundColor: colors.surface200, elevation: 0, title: Text('الإعدادات', style: textTheme.titleMedium)),
      body: ListView(
        padding: const EdgeInsets.all(AppSpacing.space4),
        children: [
          Text('المظهر', style: textTheme.titleMedium),
          const SizedBox(height: AppSpacing.space2),
          _Option(label: 'فاتح', selected: themeMode == ThemeMode.light, colors: colors, textTheme: textTheme, onTap: () async {
            ref.read(themeModeProvider.notifier).state = ThemeMode.light;
            await AppPrefs.saveThemeMode(ThemeMode.light);
          }),
          _Option(label: 'داكن', selected: themeMode == ThemeMode.dark, colors: colors, textTheme: textTheme, onTap: () async {
            ref.read(themeModeProvider.notifier).state = ThemeMode.dark;
            await AppPrefs.saveThemeMode(ThemeMode.dark);
          }),
          _Option(label: 'تلقائي (حسب الجهاز)', selected: themeMode == ThemeMode.system, colors: colors, textTheme: textTheme, onTap: () async {
            ref.read(themeModeProvider.notifier).state = ThemeMode.system;
            await AppPrefs.saveThemeMode(ThemeMode.system);
          }),

          const SizedBox(height: AppSpacing.space5),
          Text('اللغة', style: textTheme.titleMedium),
          const SizedBox(height: AppSpacing.space2),
          _Option(label: 'العربية', selected: locale.languageCode == 'ar', colors: colors, textTheme: textTheme, onTap: () async {
            const l = Locale('ar');
            ref.read(localeProvider.notifier).state = l;
            await AppPrefs.saveLocale(l);
          }),
          _Option(label: 'English', selected: locale.languageCode == 'en', colors: colors, textTheme: textTheme, onTap: () async {
            const l = Locale('en');
            ref.read(localeProvider.notifier).state = l;
            await AppPrefs.saveLocale(l);
          }),
          _Option(label: 'کوردی سۆرانی', selected: false, enabled: false, trailing: 'قريباً', colors: colors, textTheme: textTheme, onTap: null),

          const SizedBox(height: AppSpacing.space5),
          Divider(color: colors.border, height: 1),
          const SizedBox(height: AppSpacing.space3),
          InkWell(
            onTap: () => SharePlus.instance.share(ShareParams(text: 'جرّب تطبيق Ride Platform لطلب رحلاتك بسهولة')),
            child: Padding(
              padding: const EdgeInsets.symmetric(vertical: AppSpacing.space3),
              child: Row(
                children: [
                  Icon(Icons.ios_share, size: 20, color: colors.ink),
                  const SizedBox(width: AppSpacing.space3),
                  Expanded(child: Text('شارك التطبيق', style: textTheme.bodyLarge)),
                  Icon(Icons.chevron_left, size: 18, color: colors.inkMuted),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _Option extends StatelessWidget {
  final String label;
  final bool selected;
  final bool enabled;
  final String? trailing;
  final VoidCallback? onTap;
  final AppColors colors;
  final TextTheme textTheme;

  const _Option({
    required this.label,
    required this.selected,
    required this.colors,
    required this.textTheme,
    this.enabled = true,
    this.trailing,
    this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: enabled ? onTap : null,
      borderRadius: BorderRadius.circular(AppRadius.sm),
      child: Container(
        margin: const EdgeInsets.only(bottom: AppSpacing.space2),
        padding: const EdgeInsets.symmetric(horizontal: AppSpacing.space3 + 2, vertical: AppSpacing.space3),
        decoration: BoxDecoration(
          color: selected ? colors.brand100 : colors.surface200,
          border: Border.all(color: selected ? colors.brand500 : colors.border),
          borderRadius: BorderRadius.circular(AppRadius.sm),
        ),
        child: Row(
          children: [
            Expanded(child: Text(label, style: textTheme.bodyLarge?.copyWith(color: enabled ? colors.ink : colors.inkMuted))),
            if (trailing != null) Text(trailing!, style: textTheme.bodySmall?.copyWith(color: colors.inkMuted)),
            if (selected) Icon(Icons.check_circle, color: colors.brand500, size: 20),
          ],
        ),
      ),
    );
  }
}
