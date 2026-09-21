import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_exception.dart';
import '../../core/api/api_messages.dart';
import '../../core/phone.dart';
import '../../state/api_providers.dart';
import '../../theme/app_theme.dart';
import 'otp_screen.dart';

class PhoneEntryScreen extends ConsumerStatefulWidget {
  const PhoneEntryScreen({super.key});

  @override
  ConsumerState<PhoneEntryScreen> createState() => _PhoneEntryScreenState();
}

class _PhoneEntryScreenState extends ConsumerState<PhoneEntryScreen> {
  final _controller = TextEditingController();
  bool _submitting = false;
  String? _error;

  bool get _isValid => normalizeIraqPhone(_controller.text) != null;

  Future<void> _sendOtp() async {
    final phone = normalizeIraqPhone(_controller.text);
    if (phone == null || _submitting) return;

    setState(() {
      _submitting = true;
      _error = null;
    });

    try {
      final challenge = await ref.read(authApiProvider).requestOtp(phone);
      if (!mounted) return;
      setState(() => _submitting = false);

      Navigator.of(context).push(
        MaterialPageRoute(
          builder: (_) => OtpScreen(
            phoneNumber: _controller.text.trim(),
            e164Phone: phone,
            challengeId: challenge.challengeId,
          ),
        ),
      );
    } on ApiException catch (error) {
      if (!mounted) return;
      setState(() {
        _submitting = false;
        _error = describeFailure(error, wrong: 'رقم الهاتف غير صالح. تأكد منه وحاول مرة أخرى');
      });
    }
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;

    return Scaffold(
      backgroundColor: colors.surface200,
      body: SafeArea(
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: AppSpacing.space5),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const SizedBox(height: AppSpacing.space6 * 2),
              Container(
                width: 56,
                height: 56,
                decoration: BoxDecoration(
                  color: colors.brand500,
                  borderRadius: BorderRadius.circular(AppRadius.sm),
                ),
                child: const Icon(
                  Icons.directions_car_filled,
                  color: Colors.white,
                ),
              ),
              const SizedBox(height: AppSpacing.space5),
              Text('أدخل رقم هاتفك', style: textTheme.titleLarge),
              const SizedBox(height: AppSpacing.space1),
              Text(
                'رح نرسلّك رمز تحقق عبر رسالة نصية',
                style: textTheme.bodyLarge?.copyWith(
                  color: colors.inkMuted,
                  fontSize: 14,
                ),
              ),
              const SizedBox(height: AppSpacing.space6),
              Container(
                decoration: BoxDecoration(
                  border: Border.all(color: colors.border),
                  borderRadius: BorderRadius.circular(AppRadius.sm),
                ),
                child: Row(
                  children: [
                    Padding(
                      padding: const EdgeInsets.symmetric(
                        horizontal: AppSpacing.space3 + 2,
                      ),
                      child: Text(
                        '+٩٦٤',
                        style: textTheme.titleMedium?.copyWith(
                          color: colors.inkMuted,
                        ),
                      ),
                    ),
                    Container(width: 1, height: 28, color: colors.border),
                    Expanded(
                      child: TextField(
                        controller: _controller,
                        keyboardType: TextInputType.phone,
                        textDirection: TextDirection.ltr,
                        onChanged: (_) => setState(() {}),
                        decoration: const InputDecoration(
                          border: InputBorder.none,
                          hintText: '٧٧٠ ١٢٣ ٤٥٦٧',
                          contentPadding: EdgeInsets.symmetric(
                            horizontal: AppSpacing.space3,
                            vertical: AppSpacing.space3 + 2,
                          ),
                        ),
                      ),
                    ),
                  ],
                ),
              ),
              if (_error != null) ...[
                const SizedBox(height: AppSpacing.space3),
                Text(
                  _error!,
                  style: textTheme.bodySmall?.copyWith(color: colors.danger),
                ),
              ],
              const SizedBox(height: AppSpacing.space5),
              ElevatedButton(
                onPressed: _isValid && !_submitting ? _sendOtp : null,
                style: ElevatedButton.styleFrom(
                  disabledBackgroundColor: colors.inkMuted,
                ),
                child: _submitting
                    ? const SizedBox(
                        width: 20,
                        height: 20,
                        child: CircularProgressIndicator(
                          strokeWidth: 2,
                          color: Colors.white,
                        ),
                      )
                    : const Text('التالي'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
