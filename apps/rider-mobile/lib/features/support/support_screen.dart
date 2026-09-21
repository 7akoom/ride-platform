import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../theme/app_theme.dart';

/// TODO(robert): swap this real WhatsApp number and support email in
/// before shipping. This deliberately does NOT build a ticketing system —
/// no service in the platform models a "support ticket" today, and a
/// proper one (queue, statuses, agent assignment) is disproportionate to
/// this stage. A WhatsApp/email deep-link to a real person is what most
/// apps at this size actually use, and it's free.
const _supportWhatsappNumber = '9647700000000'; // no leading +, no spaces
const _supportEmail = 'support@example.com';

class SupportScreen extends StatelessWidget {
  final String? tripContext;

  const SupportScreen({super.key, this.tripContext});

  Future<void> _openWhatsapp() async {
    final text = tripContext != null
        ? 'مرحباً، عندي استفسار بخصوص رحلة إلى $tripContext'
        : 'مرحباً، عندي استفسار بخصوص التطبيق';
    final uri = Uri.parse(
      'https://wa.me/$_supportWhatsappNumber?text=${Uri.encodeComponent(text)}',
    );
    await launchUrl(uri, mode: LaunchMode.externalApplication);
  }

  Future<void> _openEmail() async {
    final subject = tripContext != null
        ? 'استفسار بخصوص رحلة إلى $tripContext'
        : 'استفسار بخصوص التطبيق';
    final uri = Uri(
      scheme: 'mailto',
      path: _supportEmail,
      query: 'subject=${Uri.encodeComponent(subject)}',
    );
    await launchUrl(uri);
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;

    return Scaffold(
      backgroundColor: colors.surface100,
      appBar: AppBar(
        backgroundColor: colors.surface200,
        elevation: 0,
        title: Text('المساعدة والدعم', style: textTheme.titleMedium),
      ),
      body: ListView(
        padding: const EdgeInsets.all(AppSpacing.space4),
        children: [
          Text(
            'كيف تحب تتواصل معنا؟',
            style: textTheme.titleMedium,
          ),
          const SizedBox(height: AppSpacing.space4),
          _ContactTile(
            icon: Icons.chat,
            label: 'واتساب',
            subtitle: 'رد أسرع، متاح خلال ساعات الدوام',
            onTap: _openWhatsapp,
            colors: colors,
            textTheme: textTheme,
          ),
          _ContactTile(
            icon: Icons.email_outlined,
            label: 'البريد الإلكتروني',
            subtitle: _supportEmail,
            onTap: _openEmail,
            colors: colors,
            textTheme: textTheme,
          ),
        ],
      ),
    );
  }
}

class _ContactTile extends StatelessWidget {
  final IconData icon;
  final String label;
  final String subtitle;
  final VoidCallback onTap;
  final AppColors colors;
  final TextTheme textTheme;

  const _ContactTile({
    required this.icon,
    required this.label,
    required this.subtitle,
    required this.onTap,
    required this.colors,
    required this.textTheme,
  });

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(AppRadius.md),
      child: Container(
        margin: const EdgeInsets.only(bottom: AppSpacing.space3),
        padding: const EdgeInsets.all(AppSpacing.space4),
        decoration: BoxDecoration(
          color: colors.surface200,
          border: Border.all(color: colors.border),
          borderRadius: BorderRadius.circular(AppRadius.md),
        ),
        child: Row(
          children: [
            Container(
              width: 44,
              height: 44,
              decoration: BoxDecoration(
                color: colors.brand100,
                borderRadius: BorderRadius.circular(AppRadius.sm),
              ),
              child: Icon(icon, color: colors.brand600, size: 20),
            ),
            const SizedBox(width: AppSpacing.space3),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(label, style: textTheme.titleMedium),
                  Text(
                    subtitle,
                    style: textTheme.bodySmall?.copyWith(
                      color: colors.inkMuted,
                    ),
                  ),
                ],
              ),
            ),
            Icon(Icons.chevron_left, size: 18, color: colors.inkMuted),
          ],
        ),
      ),
    );
  }
}
