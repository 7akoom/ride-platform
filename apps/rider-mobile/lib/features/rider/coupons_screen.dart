import 'package:flutter/material.dart';

import '../../theme/app_theme.dart';

class Coupon {
  final String code;
  final String description;
  final String expiry;

  const Coupon({
    required this.code,
    required this.description,
    required this.expiry,
  });
}

// TODO(robert): replace with Pricing.GetCoupon / a future list-coupons RPC
// through the Gateway — pricing-service already has coupon/first-ride/
// loyalty discount logic server-side, this screen just needs to surface it.
const _demoCoupons = [
  Coupon(
    code: 'WELCOME10',
    description: 'خصم ١٠٪ على أول رحلة',
    expiry: 'صالح حتى ٣٠ سبتمبر',
  ),
  Coupon(
    code: 'RAMADAN25',
    description: 'خصم ٢٥٪ على الرحلات المسائية',
    expiry: 'صالح حتى نهاية الشهر',
  ),
];

class CouponsScreen extends StatefulWidget {
  const CouponsScreen({super.key});

  @override
  State<CouponsScreen> createState() => _CouponsScreenState();
}

class _CouponsScreenState extends State<CouponsScreen> {
  final _controller = TextEditingController();

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
      backgroundColor: colors.surface100,
      appBar: AppBar(
        backgroundColor: colors.surface200,
        elevation: 0,
        title: Text('الكوبونات', style: textTheme.titleMedium),
      ),
      body: ListView(
        padding: const EdgeInsets.all(AppSpacing.space4),
        children: [
          Row(
            children: [
              Expanded(
                child: TextField(
                  controller: _controller,
                  textDirection: TextDirection.ltr,
                  decoration: InputDecoration(
                    hintText: 'أدخل رمز الكوبون',
                    enabledBorder: OutlineInputBorder(
                      borderRadius: BorderRadius.circular(AppRadius.sm),
                      borderSide: BorderSide(color: colors.border),
                    ),
                    border: OutlineInputBorder(
                      borderRadius: BorderRadius.circular(AppRadius.sm),
                      borderSide: BorderSide(color: colors.border),
                    ),
                    focusedBorder: OutlineInputBorder(
                      borderRadius: BorderRadius.circular(AppRadius.sm),
                      borderSide: BorderSide(
                        color: colors.brand500,
                        width: 2,
                      ),
                    ),
                  ),
                ),
              ),
              const SizedBox(width: AppSpacing.space2),
              SizedBox(
                height: 48,
                child: ElevatedButton(
                  onPressed: () {},
                  child: const Text('تفعيل'),
                ),
              ),
            ],
          ),
          const SizedBox(height: AppSpacing.space5),
          Text('كوبوناتك المتاحة', style: textTheme.titleMedium),
          const SizedBox(height: AppSpacing.space3),
          for (final coupon in _demoCoupons)
            Container(
              margin: const EdgeInsets.only(bottom: AppSpacing.space3),
              padding: const EdgeInsets.all(AppSpacing.space4),
              decoration: BoxDecoration(
                color: colors.surface200,
                border: Border.all(color: colors.border, style: BorderStyle.solid),
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
                    child: Icon(
                      Icons.local_offer_outlined,
                      color: colors.brand600,
                      size: 20,
                    ),
                  ),
                  const SizedBox(width: AppSpacing.space3),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          coupon.code,
                          style: textTheme.titleMedium,
                          textDirection: TextDirection.ltr,
                        ),
                        Text(
                          coupon.description,
                          style: textTheme.bodySmall?.copyWith(
                            color: colors.inkMuted,
                          ),
                        ),
                        Text(
                          coupon.expiry,
                          style: textTheme.bodySmall?.copyWith(
                            color: colors.warning,
                          ),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}
