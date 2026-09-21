import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_exception.dart';
import '../../core/api/api_messages.dart';
import '../../core/models/driver_profile.dart';
import '../../state/api_providers.dart';
import '../../state/session_storage.dart';
import '../../theme/app_theme.dart';
import '../onboarding/account_router.dart';

/// The one form a new driver fills in: their name and their car. The account starts as
/// "pending": an operator approves it before the driver can go online.
class DriverRegistrationScreen extends ConsumerStatefulWidget {
  const DriverRegistrationScreen({super.key});

  @override
  ConsumerState<DriverRegistrationScreen> createState() => _DriverRegistrationScreenState();
}

class _DriverRegistrationScreenState extends ConsumerState<DriverRegistrationScreen> {
  final _firstName = TextEditingController();
  final _lastName = TextEditingController();
  final _make = TextEditingController();
  final _model = TextEditingController();
  final _color = TextEditingController();
  final _plate = TextEditingController();

  String _vehicleClass = 'economy';
  bool _submitting = false;
  String? _error;

  bool get _isValid =>
      _firstName.text.trim().length >= 2 &&
      _lastName.text.trim().length >= 2 &&
      _make.text.trim().length >= 2 &&
      _model.text.trim().isNotEmpty &&
      _color.text.trim().length >= 2 &&
      _plate.text.trim().length >= 3;

  @override
  void dispose() {
    _firstName.dispose();
    _lastName.dispose();
    _make.dispose();
    _model.dispose();
    _color.dispose();
    _plate.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
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

      final profile = await ref.read(driverApiProvider).create(
            identityId: identityId,
            displayName: '${_firstName.text.trim()} ${_lastName.text.trim()}',
            vehicle: Vehicle(
              make: _make.text.trim(),
              model: _model.text.trim(),
              color: _color.text.trim(),
              plateNumber: _plate.text.trim(),
              vehicleClass: _vehicleClass,
            ),
          );

      await SessionStorage.saveDriverId(profile.id);
      ref.invalidate(driverProfileProvider);

      final account = await ref.read(accountServiceProvider).resolve();
      if (!mounted) return;

      Navigator.of(context).pushAndRemoveUntil(
        MaterialPageRoute(builder: (_) => screenForAccount(account)),
        (route) => false,
      );
    } on ApiException catch (error) {
      if (!mounted) return;
      setState(() {
        _submitting = false;
        _error = describeFailure(error, wrong: 'تعذر تسجيل بياناتك. تأكد منها وحاول مرة أخرى');
      });
    }
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

  Widget _field(String label, TextEditingController controller, String hint, TextTheme textTheme) {
    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.space4),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(label, style: textTheme.labelLarge),
          const SizedBox(height: AppSpacing.space2),
          TextField(
            controller: controller,
            onChanged: (_) => setState(() {}),
            textInputAction: TextInputAction.next,
            decoration: _decoration(context, hint),
          ),
        ],
      ),
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
              Text('سجّل كسائق', style: textTheme.titleLarge),
              const SizedBox(height: AppSpacing.space1),
              Text('أدخل بياناتك وبيانات مركبتك، ونراجع طلبك بأقرب وقت', style: textTheme.bodyLarge?.copyWith(color: colors.inkMuted, fontSize: 14)),
              const SizedBox(height: AppSpacing.space6),
              _field('الاسم الأول', _firstName, 'مثال: أحمد', textTheme),
              _field('الاسم الأخير', _lastName, 'مثال: كريم', textTheme),
              Text('مركبتك', style: textTheme.titleMedium),
              const SizedBox(height: AppSpacing.space3),
              _field('الشركة المصنّعة', _make, 'مثال: تويوتا', textTheme),
              _field('الموديل', _model, 'مثال: كورولا', textTheme),
              _field('اللون', _color, 'مثال: أبيض', textTheme),
              _field('رقم اللوحة', _plate, 'مثال: 33452', textTheme),
              Text('فئة المركبة', style: textTheme.labelLarge),
              const SizedBox(height: AppSpacing.space2),
              Row(
                children: [
                  Expanded(
                    child: _ClassChip(
                      label: 'اقتصادي',
                      selected: _vehicleClass == 'economy',
                      colors: colors,
                      onTap: () => setState(() => _vehicleClass = 'economy'),
                    ),
                  ),
                  const SizedBox(width: AppSpacing.space3),
                  Expanded(
                    child: _ClassChip(
                      label: 'مريح',
                      selected: _vehicleClass == 'comfort',
                      colors: colors,
                      onTap: () => setState(() => _vehicleClass = 'comfort'),
                    ),
                  ),
                ],
              ),
              if (_error != null) ...[
                const SizedBox(height: AppSpacing.space3),
                Text(_error!, style: textTheme.bodySmall?.copyWith(color: colors.danger)),
              ],
              const SizedBox(height: AppSpacing.space6),
              ElevatedButton(
                onPressed: _isValid && !_submitting ? _submit : null,
                style: ElevatedButton.styleFrom(disabledBackgroundColor: colors.inkMuted),
                child: _submitting
                    ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                    : const Text('إرسال الطلب'),
              ),
              const SizedBox(height: AppSpacing.space6),
            ],
          ),
        ),
      ),
    );
  }
}

class _ClassChip extends StatelessWidget {
  final String label;
  final bool selected;
  final AppColors colors;
  final VoidCallback onTap;

  const _ClassChip({
    required this.label,
    required this.selected,
    required this.colors,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(AppRadius.sm),
      child: Container(
        height: 44,
        alignment: Alignment.center,
        decoration: BoxDecoration(
          color: selected ? colors.brand100 : colors.surface200,
          border: Border.all(color: selected ? colors.brand500 : colors.border, width: 1.5),
          borderRadius: BorderRadius.circular(AppRadius.sm),
        ),
        child: Text(label, style: TextStyle(fontWeight: FontWeight.w600, fontSize: 14, color: colors.ink)),
      ),
    );
  }
}
