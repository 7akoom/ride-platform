import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../state/app_prefs.dart';
import '../../state/session_storage.dart';
import '../../state/theme_provider.dart';
import '../auth/phone_entry_screen.dart';
import '../rider/request_ride_screen.dart';

class ThemeSelectScreen extends ConsumerStatefulWidget {
  const ThemeSelectScreen({super.key});

  @override
  ConsumerState<ThemeSelectScreen> createState() => _ThemeSelectScreenState();
}

class _ThemeSelectScreenState extends ConsumerState<ThemeSelectScreen> {
  ThemeMode _selected = ThemeMode.system;

  Future<void> _finish() async {
    ref.read(themeModeProvider.notifier).state = _selected;
    await AppPrefs.saveThemeMode(_selected);
    await AppPrefs.setOnboarded();

    if (!mounted) return;
    final token = await SessionStorage.readToken();
    if (!mounted) return;
    Navigator.of(context).pushAndRemoveUntil(
      MaterialPageRoute(
        builder: (_) =>
            token != null ? const RequestRideScreen() : const PhoneEntryScreen(),
      ),
      (route) => false,
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.white,
      body: SafeArea(
        child: Column(
          children: [
            Expanded(
              child: Center(
                child: Row(
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: [
                    _PreviewCard(dark: false, selected: _selected == ThemeMode.light),
                    const SizedBox(width: 18),
                    _PreviewCard(dark: true, selected: _selected == ThemeMode.dark),
                  ],
                ),
              ),
            ),
            Padding(
              padding: const EdgeInsets.fromLTRB(24, 28, 24, 32),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  const Column(
                    children: [
                      Text('شو المظهر اللي يناسبك؟', style: TextStyle(fontSize: 20, fontWeight: FontWeight.w600, color: Color(0xFF1B2430))),
                      SizedBox(height: 4),
                      Text('تقدر تغيّره بأي وقت من الإعدادات', style: TextStyle(fontSize: 14, color: Color(0xFF5B6672))),
                    ],
                  ),
                  const SizedBox(height: 22),
                  _ModeOption(label: 'فاتح', selected: _selected == ThemeMode.light, onTap: () => setState(() => _selected = ThemeMode.light)),
                  const SizedBox(height: 10),
                  _ModeOption(label: 'داكن', selected: _selected == ThemeMode.dark, onTap: () => setState(() => _selected = ThemeMode.dark)),
                  const SizedBox(height: 10),
                  _ModeOption(label: 'تلقائي (حسب الجهاز)', selected: _selected == ThemeMode.system, onTap: () => setState(() => _selected = ThemeMode.system)),
                  const SizedBox(height: 22),
                  ElevatedButton(onPressed: _finish, child: const Text('استمرار')),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _PreviewCard extends StatelessWidget {
  final bool dark;
  final bool selected;

  const _PreviewCard({required this.dark, required this.selected});

  @override
  Widget build(BuildContext context) {
    final bg = dark ? const Color(0xFF14181D) : const Color(0xFFF1F3F4);
    final accent = dark ? const Color(0xFF17A3A1) : const Color(0xFF0E7C7B);
    final line1 = dark ? const Color(0xFFE7EBEF) : const Color(0xFF1B2430);
    final line2 = dark ? const Color(0xFF2A3038) : const Color(0xFFDBE0E4);

    return Container(
      width: 92,
      height: 150,
      decoration: BoxDecoration(
        color: bg,
        borderRadius: BorderRadius.circular(14),
        border: Border.all(color: selected ? const Color(0xFF0E7C7B) : Colors.transparent, width: 2),
        boxShadow: const [BoxShadow(color: Color(0x14000000), blurRadius: 10, offset: Offset(0, 2))],
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(height: 45, decoration: BoxDecoration(color: accent, borderRadius: const BorderRadius.only(topLeft: Radius.circular(12), topRight: Radius.circular(12)))),
          Padding(
            padding: const EdgeInsets.all(8),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Container(width: 60, height: 6, decoration: BoxDecoration(color: line1, borderRadius: BorderRadius.circular(3))),
                const SizedBox(height: 5),
                Container(width: 38, height: 6, decoration: BoxDecoration(color: line2, borderRadius: BorderRadius.circular(3))),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _ModeOption extends StatelessWidget {
  final String label;
  final bool selected;
  final VoidCallback onTap;

  const _ModeOption({required this.label, required this.selected, required this.onTap});

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(8),
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 16),
        decoration: BoxDecoration(
          color: selected ? const Color(0xFFC9E8E6) : Colors.white,
          border: Border.all(color: selected ? const Color(0xFF0E7C7B) : const Color(0xFFDBE0E4), width: 1.5),
          borderRadius: BorderRadius.circular(8),
        ),
        child: Row(
          children: [
            Expanded(child: Text(label, style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600, color: Color(0xFF1B2430)))),
            Container(
              width: 20,
              height: 20,
              decoration: BoxDecoration(
                shape: BoxShape.circle,
                color: selected ? const Color(0xFF0E7C7B) : Colors.white,
                border: Border.all(color: selected ? const Color(0xFF0E7C7B) : const Color(0xFFDBE0E4), width: 1.5),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
