import 'package:flutter/material.dart';

import '../../theme/app_theme.dart';

/// TODO(robert): Wallet has no peer-to-peer transfer RPC yet — needs a new
/// endpoint that looks a phone number up via Identity, then moves funds
/// between the two riders' wallets as one atomic ledger transaction. Wired
/// here as: type phone -> (fake) recipient lookup -> amount -> confirm.
class SendMoneyScreen extends StatefulWidget {
  const SendMoneyScreen({super.key});

  @override
  State<SendMoneyScreen> createState() => _SendMoneyScreenState();
}

class _SendMoneyScreenState extends State<SendMoneyScreen> {
  final _phone = TextEditingController();
  final _amount = TextEditingController();
  final _note = TextEditingController();
  bool _recipientFound = false;

  void _checkRecipient(String value) {
    final found = value.trim().length >= 10;
    if (found != _recipientFound) setState(() => _recipientFound = found);
  }

  void _send() {
    showDialog(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('تم إرسال الطلب'),
        content: Text('رح يتم تحويل ${_amount.text.isEmpty ? "المبلغ" : _amount.text} د.ع بعد ربط الميزة بالباكند.'),
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
    _note.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;

    return Scaffold(
      backgroundColor: colors.surface200,
      appBar: AppBar(backgroundColor: colors.surface200, elevation: 0, title: Text('إرسال إلى مستخدم', style: textTheme.titleMedium)),
      body: Padding(
        padding: const EdgeInsets.all(AppSpacing.space5),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text('رقم هاتف المستقبل', style: textTheme.labelLarge),
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
                      hintText: '770 123 4567',
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
                    CircleAvatar(radius: 22, backgroundColor: colors.brand100, child: Text('أ', style: TextStyle(color: colors.brand600, fontWeight: FontWeight.w600))),
                    const SizedBox(width: AppSpacing.space3),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text('أحمد كريم', style: textTheme.titleMedium),
                          Text('مستخدم على Ride Platform', style: textTheme.bodySmall?.copyWith(color: colors.inkMuted)),
                        ],
                      ),
                    ),
                    Icon(Icons.check_circle, color: colors.success),
                  ],
                ),
              ),
            const SizedBox(height: AppSpacing.space5),
            Text('المبلغ', style: textTheme.labelLarge),
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
              controller: _note,
              decoration: InputDecoration(
                hintText: 'ملاحظة (اختياري)',
                enabledBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(AppRadius.sm), borderSide: BorderSide(color: colors.border)),
                border: OutlineInputBorder(borderRadius: BorderRadius.circular(AppRadius.sm), borderSide: BorderSide(color: colors.border)),
              ),
            ),
            const Spacer(),
            ElevatedButton(
              onPressed: _recipientFound && _amount.text.isNotEmpty ? _send : null,
              style: ElevatedButton.styleFrom(disabledBackgroundColor: colors.inkMuted),
              child: const Text('إرسال'),
            ),
          ],
        ),
      ),
    );
  }
}
