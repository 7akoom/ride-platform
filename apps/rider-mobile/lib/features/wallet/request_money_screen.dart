import 'package:flutter/material.dart';

import '../../theme/app_theme.dart';

/// TODO(robert): same backend gap as SendMoneyScreen, plus this needs a
/// "pending request" state in Wallet's ledger and a push notification to
/// the other rider (Notification-service already has the delivery
/// mechanism — this just needs to trigger it) so they see it to accept.
class RequestMoneyScreen extends StatefulWidget {
  const RequestMoneyScreen({super.key});

  @override
  State<RequestMoneyScreen> createState() => _RequestMoneyScreenState();
}

class _RequestMoneyScreenState extends State<RequestMoneyScreen> {
  final _phone = TextEditingController();
  final _amount = TextEditingController();
  final _reason = TextEditingController();
  bool _recipientFound = false;

  void _checkRecipient(String value) {
    final found = value.trim().length >= 10;
    if (found != _recipientFound) setState(() => _recipientFound = found);
  }

  void _request() {
    showDialog(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('تم إرسال الطلب'),
        content: const Text('رح يوصل إشعار للطرف التاني بعد ربط الميزة بالباكند.'),
        actions: [
          TextButton(onPressed: () { Navigator.of(context).pop(); Navigator.of(context).pop(); }, child: const Text('حسناً')),
        ],
      ),
    );
  }

  @override
  void dispose() {
    _phone.dispose();
    _amount.dispose();
    _reason.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;

    return Scaffold(
      backgroundColor: colors.surface200,
      appBar: AppBar(backgroundColor: colors.surface200, elevation: 0, title: Text('طلب رصيد', style: textTheme.titleMedium)),
      body: Padding(
        padding: const EdgeInsets.all(AppSpacing.space5),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text('من رقم هاتف', style: textTheme.labelLarge),
            const SizedBox(height: AppSpacing.space2),
            Row(
              children: [
                Container(
                  padding: const EdgeInsets.symmetric(horizontal: AppSpacing.space3 + 2, vertical: AppSpacing.space3 + 2),
                  decoration: BoxDecoration(border: Border.all(color: colors.border), borderRadius: BorderRadius.circular(AppRadius.sm)),
                  child: Text('+٩٦٤', style: TextStyle(color: colors.inkMuted)),
                ),
                const SizedBox(width: AppSpacing.space2),
                Expanded(
                  child: TextField(
                    controller: _phone,
                    keyboardType: TextInputType.phone,
                    textDirection: TextDirection.ltr,
                    onChanged: _checkRecipient,
                    decoration: InputDecoration(
                      hintText: '750 987 6543',
                      enabledBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(AppRadius.sm), borderSide: BorderSide(color: colors.border)),
                      border: OutlineInputBorder(borderRadius: BorderRadius.circular(AppRadius.sm), borderSide: BorderSide(color: colors.border)),
                      focusedBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(AppRadius.sm), borderSide: BorderSide(color: colors.brand500, width: 2)),
                    ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: AppSpacing.space4),
            if (_recipientFound)
              Container(
                padding: const EdgeInsets.all(AppSpacing.space3 + 2),
                decoration: BoxDecoration(color: colors.surface100, borderRadius: BorderRadius.circular(AppRadius.md)),
                child: Row(
                  children: [
                    CircleAvatar(radius: 22, backgroundColor: colors.brand100, child: Text('س', style: TextStyle(color: colors.brand600, fontWeight: FontWeight.w600))),
                    const SizedBox(width: AppSpacing.space3),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text('سارة أحمد', style: textTheme.titleMedium),
                          Text('مستخدم على Ride Platform', style: textTheme.bodySmall?.copyWith(color: colors.inkMuted)),
                        ],
                      ),
                    ),
                    Icon(Icons.check_circle, color: colors.success),
                  ],
                ),
              ),
            const SizedBox(height: AppSpacing.space5),
            Text('المبلغ المطلوب', style: textTheme.labelLarge),
            const SizedBox(height: AppSpacing.space2),
            TextField(
              controller: _amount,
              keyboardType: TextInputType.number,
              textAlign: TextAlign.center,
              style: textTheme.displayMedium,
              onChanged: (_) => setState(() {}),
              decoration: const InputDecoration(border: InputBorder.none, hintText: '٠', suffixText: 'د.ع'),
            ),
            const SizedBox(height: AppSpacing.space4),
            TextField(
              controller: _reason,
              decoration: InputDecoration(
                hintText: 'سبب الطلب (اختياري) — مثال: نصف أجرة الرحلة',
                enabledBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(AppRadius.sm), borderSide: BorderSide(color: colors.border)),
                border: OutlineInputBorder(borderRadius: BorderRadius.circular(AppRadius.sm), borderSide: BorderSide(color: colors.border)),
              ),
            ),
            const SizedBox(height: AppSpacing.space4),
            Container(
              padding: const EdgeInsets.all(AppSpacing.space3),
              decoration: BoxDecoration(color: colors.brand100, borderRadius: BorderRadius.circular(AppRadius.sm)),
              child: Row(
                children: [
                  Icon(Icons.info_outline, size: 16, color: colors.brand600),
                  const SizedBox(width: AppSpacing.space2),
                  Expanded(
                    child: Text('بيوصلها إشعار بطلبك، وبتقدر تقبله أو ترفضه من عندها', style: textTheme.bodySmall?.copyWith(color: colors.brand600)),
                  ),
                ],
              ),
            ),
            const Spacer(),
            ElevatedButton(
              onPressed: _recipientFound && _amount.text.isNotEmpty ? _request : null,
              style: ElevatedButton.styleFrom(disabledBackgroundColor: colors.inkMuted),
              child: const Text('إرسال طلب'),
            ),
          ],
        ),
      ),
    );
  }
}
