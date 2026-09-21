import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_exception.dart';
import '../../core/api/api_messages.dart';
import '../../state/api_providers.dart';
import '../../state/session_storage.dart';
import '../../theme/app_theme.dart';
import '../support/support_screen.dart';

class ProfileScreen extends ConsumerWidget {
  const ProfileScreen({super.key});

  Future<void> _rename(BuildContext context, WidgetRef ref, String current) async {
    final name = await showDialog<String>(
      context: context,
      builder: (_) => _RenameDialog(current: current),
    );

    if (name == null || name.length < 2 || name == current) return;

    try {
      final riderId = await SessionStorage.readRiderId();
      if (riderId == null) return;

      await ref.read(riderApiProvider).rename(riderId: riderId, displayName: name);
      ref.invalidate(riderProfileProvider);

      if (!context.mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('تم حفظ الاسم')));
    } on ApiException catch (error) {
      if (!context.mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(describeFailure(error, wrong: 'تعذر حفظ الاسم. تأكد منه وحاول مرة أخرى'))),
      );
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;
    final profile = ref.watch(riderProfileProvider).valueOrNull;
    final phone = ref.watch(myPhoneProvider).valueOrNull;

    return Scaffold(
      backgroundColor: colors.surface100,
      appBar: AppBar(
        backgroundColor: colors.surface200,
        elevation: 0,
        title: Text('الملف الشخصي', style: textTheme.titleMedium),
      ),
      body: ListView(
        padding: const EdgeInsets.all(AppSpacing.space4),
        children: [
          Container(
            padding: const EdgeInsets.all(AppSpacing.space4),
            decoration: BoxDecoration(
              color: colors.surface200,
              borderRadius: BorderRadius.circular(AppRadius.md),
              border: Border.all(color: colors.border),
            ),
            child: Row(
              children: [
                CircleAvatar(
                  radius: 28,
                  backgroundColor: colors.brand100,
                  child: Text(
                    profile?.initial ?? '',
                    style: textTheme.titleLarge?.copyWith(
                      color: colors.brand600,
                    ),
                  ),
                ),
                const SizedBox(width: AppSpacing.space3),
                Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(profile?.displayName ?? '', style: textTheme.titleMedium),
                    const SizedBox(height: 2),
                    Text(
                      phone ?? '',
                      style: textTheme.bodySmall?.copyWith(
                        color: colors.inkMuted,
                      ),
                      textDirection: TextDirection.ltr,
                    ),
                  ],
                ),
              ],
            ),
          ),
          const SizedBox(height: AppSpacing.space4),
          _MenuTile(
            icon: Icons.person_outline,
            label: 'تعديل الاسم',
            onTap: () => _rename(context, ref, profile?.displayName ?? ''),
            colors: colors,
            textTheme: textTheme,
          ),
          _MenuTile(
            icon: Icons.language_outlined,
            label: 'اللغة',
            trailing: 'العربية',
            onTap: () {},
            colors: colors,
            textTheme: textTheme,
          ),
          _MenuTile(
            icon: Icons.help_outline,
            label: 'المساعدة والدعم',
            onTap: () => Navigator.of(context).push(
              MaterialPageRoute(builder: (_) => const SupportScreen()),
            ),
            colors: colors,
            textTheme: textTheme,
          ),
          const SizedBox(height: AppSpacing.space4),
          OutlinedButton(
            onPressed: () => signOut(ref),
            style: OutlinedButton.styleFrom(
              foregroundColor: colors.danger,
              side: BorderSide(color: colors.danger, width: 1.5),
            ),
            child: const Text('تسجيل الخروج'),
          ),
        ],
      ),
    );
  }
}

class _MenuTile extends StatelessWidget {
  final IconData icon;
  final String label;
  final String? trailing;
  final VoidCallback onTap;
  final AppColors colors;
  final TextTheme textTheme;

  const _MenuTile({
    required this.icon,
    required this.label,
    required this.onTap,
    required this.colors,
    required this.textTheme,
    this.trailing,
  });

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: AppSpacing.space3),
        child: Row(
          children: [
            Icon(icon, size: 20, color: colors.ink),
            const SizedBox(width: AppSpacing.space3),
            Expanded(child: Text(label, style: textTheme.bodyLarge)),
            if (trailing != null)
              Text(
                trailing!,
                style: textTheme.bodySmall?.copyWith(color: colors.inkMuted),
              ),
            const SizedBox(width: AppSpacing.space1),
            Icon(Icons.chevron_left, size: 18, color: colors.inkMuted),
          ],
        ),
      ),
    );
  }
}

/// The name dialog owns its text controller, so the controller lives exactly as long as the
/// dialog's text field, including the moment the dialog is closing. (Disposing it from the
/// screen that opened the dialog, the moment the dialog returns, breaks that field while it
/// is still on screen.)
class _RenameDialog extends StatefulWidget {
  final String current;

  const _RenameDialog({required this.current});

  @override
  State<_RenameDialog> createState() => _RenameDialogState();
}

class _RenameDialogState extends State<_RenameDialog> {
  late final TextEditingController _controller = TextEditingController(text: widget.current);

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('تعديل الاسم'),
      content: TextField(
        controller: _controller,
        autofocus: true,
        maxLength: 60,
        textInputAction: TextInputAction.done,
        onSubmitted: (value) => Navigator.of(context).pop(value.trim()),
        decoration: const InputDecoration(hintText: 'الاسم الكامل'),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('إلغاء'),
        ),
        TextButton(
          onPressed: () => Navigator.of(context).pop(_controller.text.trim()),
          child: const Text('حفظ'),
        ),
      ],
    );
  }
}
