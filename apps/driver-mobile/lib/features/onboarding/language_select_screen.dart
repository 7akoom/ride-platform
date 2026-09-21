import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../state/app_prefs.dart';
import '../../state/locale_provider.dart';
import 'theme_select_screen.dart';

const _continueLabels = {'ar': 'استمرار', 'ku': 'بەردەوامبە', 'en': 'Continue'};

class LanguageSelectScreen extends ConsumerStatefulWidget {
  const LanguageSelectScreen({super.key});

  @override
  ConsumerState<LanguageSelectScreen> createState() => _LanguageSelectScreenState();
}

class _LanguageSelectScreenState extends ConsumerState<LanguageSelectScreen> {
  String _selected = 'ar';

  Future<void> _continue() async {
    if (_selected != 'ku') {
      // 'ku' has no GlobalMaterialLocalizations delegate yet — see the
      // note on SettingsScreen. Only ar/en actually apply here.
      final locale = Locale(_selected);
      ref.read(localeProvider.notifier).state = locale;
      await AppPrefs.saveLocale(locale);
    }
    if (!mounted) return;
    Navigator.of(context).push(
      MaterialPageRoute(builder: (_) => const ThemeSelectScreen()),
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
              child: Container(
                color: const Color(0xFFF1F3F4),
                child: Center(
                  child: SizedBox(
                    width: 72,
                    height: 72,
                    child: CustomPaint(painter: _MarkPainter()),
                  ),
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
                      Text('اختر لغتك', style: TextStyle(fontSize: 20, fontWeight: FontWeight.w600, color: Color(0xFF1B2430))),
                      Text('زمانەکەت هەڵبژێرە', style: TextStyle(fontSize: 20, fontWeight: FontWeight.w600, color: Color(0xFF1B2430))),
                      Text('Choose your language', style: TextStyle(fontSize: 20, fontWeight: FontWeight.w600, color: Color(0xFF1B2430))),
                    ],
                  ),
                  const SizedBox(height: 22),
                  _LangOption(label: 'العربية', id: 'ar', selected: _selected == 'ar', onTap: () => setState(() => _selected = 'ar')),
                  const SizedBox(height: 10),
                  _LangOption(label: 'کوردی سۆرانی', id: 'ku', selected: _selected == 'ku', onTap: () => setState(() => _selected = 'ku')),
                  const SizedBox(height: 10),
                  _LangOption(label: 'English', id: 'en', selected: _selected == 'en', onTap: () => setState(() => _selected = 'en'), ltr: true),
                  const SizedBox(height: 22),
                  ElevatedButton(
                    onPressed: _continue,
                    child: Text(_continueLabels[_selected]!),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _LangOption extends StatelessWidget {
  final String label;
  final String id;
  final bool selected;
  final VoidCallback onTap;
  final bool ltr;

  const _LangOption({
    required this.label,
    required this.id,
    required this.selected,
    required this.onTap,
    this.ltr = false,
  });

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
            Expanded(
              child: Text(
                label,
                textDirection: ltr ? TextDirection.ltr : TextDirection.rtl,
                style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600, color: Color(0xFF1B2430)),
              ),
            ),
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

class _MarkPainter extends CustomPainter {
  @override
  void paint(Canvas canvas, Size size) {
    final c = Offset(size.width / 2, size.height / 2);
    canvas.drawCircle(c, size.width / 2, Paint()..color = const Color(0xFFC9E8E6));
    final path = Path()..moveTo(20, 40)..quadraticBezierTo(36, 16, 52, 32);
    canvas.drawPath(path, Paint()..color = const Color(0xFF0E7C7B)..style = PaintingStyle.stroke..strokeWidth = 2.5);
    canvas.drawCircle(const Offset(20, 40), 4, Paint()..color = const Color(0xFF0A5F5E));
    canvas.drawCircle(const Offset(52, 32), 5, Paint()..color = const Color(0xFF0E7C7B));
  }

  @override
  bool shouldRepaint(covariant CustomPainter oldDelegate) => false;
}
