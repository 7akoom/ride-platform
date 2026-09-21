import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_exception.dart';
import '../../core/api/api_messages.dart';
import '../../state/api_providers.dart';
import '../../state/session_storage.dart';
import '../../theme/app_theme.dart';
import '../rider/request_ride_screen.dart';

/// The one step a first-time rider does after the login code: their name.
///
/// The email field that used to be here is gone on purpose: the backend keeps a rider's
/// name only, and an email has to be verified with a code before it can be linked to the
/// account. It comes back with that flow.
class ProfileSetupScreen extends ConsumerStatefulWidget {
  const ProfileSetupScreen({super.key});

  @override
  ConsumerState<ProfileSetupScreen> createState() => _ProfileSetupScreenState();
}

class _ProfileSetupScreenState extends ConsumerState<ProfileSetupScreen> {
  final _firstName = TextEditingController();
  final _lastName = TextEditingController();
  bool _submitting = false;
  String? _error;

  bool get _isValid =>
      _firstName.text.trim().length >= 2 && _lastName.text.trim().length >= 2;

  Future<void> _continue() async {
    if (!_isValid || _submitting) return;
    setState(() {
      _submitting = true;
      _error = null;
    });

    try {
      final identityId = await SessionStorage.readIdentityId();
      if (identityId == null) {
        throw const ApiException(statusCode: 401, message: 'there is no signed-in identity');
      }

      final profile = await ref.read(riderApiProvider).create(
            identityId: identityId,
            displayName: '${_firstName.text.trim()} ${_lastName.text.trim()}',
          );

      await SessionStorage.saveRiderId(profile.id);
      ref.invalidate(riderProfileProvider);

      if (!mounted) return;
      Navigator.of(context).pushAndRemoveUntil(
        MaterialPageRoute(builder: (_) => const RequestRideScreen()),
        (route) => false,
      );
    } on ApiException catch (error) {
      if (!mounted) return;
      setState(() {
        _submitting = false;
        _error = describeFailure(error, wrong: 'تعذر حفظ الملف الشخصي. تأكد من الاسم وحاول مرة أخرى');
      });
    }
  }

  @override
  void dispose() {
    _firstName.dispose();
    _lastName.dispose();
    super.dispose();
  }

  InputDecoration _decoration(BuildContext context, String hint) {
    final colors = context.colors;
    return InputDecoration(
      hintText: hint,
      enabledBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(AppRadius.sm), borderSide: BorderSide(color: colors.border)),
      border: OutlineInputBorder(borderRadius: BorderRadius.circular(AppRadius.sm), borderSide: BorderSide(color: colors.border)),
      focusedBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(AppRadius.sm), borderSide: BorderSide(color: colors.brand500, width: 2)),
      contentPadding: const EdgeInsets.symmetric(horizontal: AppSpacing.space3 + 2, vertical: AppSpacing.space3 + 2),
    );
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;

    return Scaffold(
      backgroundColor: colors.surface200,
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.symmetric(horizontal: AppSpacing.space5),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const SizedBox(height: AppSpacing.space6 * 2),
              Text('أكمل ملفك الشخصي', style: textTheme.titleLarge),
              const SizedBox(height: AppSpacing.space1),
              Text('خطوة واحدة وبنكون جاهزين', style: textTheme.bodyLarge?.copyWith(color: colors.inkMuted, fontSize: 14)),
              const SizedBox(height: AppSpacing.space6),

              Text('الاسم الأول', style: textTheme.labelLarge),
              const SizedBox(height: AppSpacing.space2),
              TextField(controller: _firstName, onChanged: (_) => setState(() {}), decoration: _decoration(context, 'مثال: أحمد')),

              const SizedBox(height: AppSpacing.space4),
              Text('الاسم الأخير', style: textTheme.labelLarge),
              const SizedBox(height: AppSpacing.space2),
              TextField(controller: _lastName, onChanged: (_) => setState(() {}), decoration: _decoration(context, 'مثال: كريم')),

              if (_error != null) ...[
                const SizedBox(height: AppSpacing.space3),
                Text(_error!, style: textTheme.bodySmall?.copyWith(color: colors.danger)),
              ],

              const SizedBox(height: AppSpacing.space6),
              ElevatedButton(
                onPressed: _isValid && !_submitting ? _continue : null,
                style: ElevatedButton.styleFrom(disabledBackgroundColor: colors.inkMuted),
                child: _submitting
                    ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                    : const Text('متابعة'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
