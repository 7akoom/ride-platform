import 'package:flutter/material.dart';

class WalletTransaction {
  final String label;
  final String date;
  final String amount;
  final bool isCredit;
  final IconData icon;

  const WalletTransaction({
    required this.label,
    required this.date,
    required this.amount,
    required this.isCredit,
    required this.icon,
  });
}

// TODO(robert): replace with Wallet.ListTransactions through the Gateway.
final List<WalletTransaction> demoWalletTransactions = [
  const WalletTransaction(label: 'رحلة · مطار أربيل الدولي', date: 'اليوم · ٤:١٢ م', amount: '−٦٬٥٠٠ د.ع', isCredit: false, icon: Icons.arrow_downward),
  const WalletTransaction(label: 'رحلة · مركز أربيل التجاري', date: 'أمس · ٧:٤٥ م', amount: '−٤٬٢٠٠ د.ع', isCredit: false, icon: Icons.arrow_downward),
  const WalletTransaction(label: 'إرسال إلى سارة أحمد', date: 'أمس · ٥:١٠ م', amount: '−٥٬٠٠٠ د.ع', isCredit: false, icon: Icons.arrow_forward),
  const WalletTransaction(label: 'شحن المحفظة', date: '١٤ سبتمبر · ١٠:٠٠ ص', amount: '+٢٠٬٠٠٠ د.ع', isCredit: true, icon: Icons.arrow_upward),
  const WalletTransaction(label: 'خصم كوبون · WELCOME10', date: '١٤ سبتمبر · ١٠:٠٠ ص', amount: '+١٬٠٠٠ د.ع', isCredit: true, icon: Icons.local_offer_outlined),
];
