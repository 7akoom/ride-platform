/// Turns 0-9 into the Arabic-Indic digits (٠-٩) the app shows.
String arabicDigits(String input) {
  final buffer = StringBuffer();

  for (final rune in input.runes) {
    if (rune >= 0x30 && rune <= 0x39) {
      buffer.writeCharCode(0x0660 + (rune - 0x30));
    } else {
      buffer.writeCharCode(rune);
    }
  }

  return buffer.toString();
}

/// "17250" and "IQD" become "١٧٬٢٥٠ د.ع". Dinars have no fractions, so it is rounded.
String formatMoney(String amount, String currencyCode) {
  final value = double.tryParse(amount) ?? 0;
  final digits = value.round().abs().toString();
  final grouped = StringBuffer();

  for (var i = 0; i < digits.length; i++) {
    if (i > 0 && (digits.length - i) % 3 == 0) {
      grouped.write('٬');
    }
    grouped.write(digits[i]);
  }

  final unit = currencyCode == 'IQD' || currencyCode.isEmpty ? 'د.ع' : currencyCode;
  final sign = value < 0 ? '-' : '';

  return arabicDigits('$sign$grouped $unit');
}

/// 187 seconds become "٤ دقائق".
String formatMinutes(double seconds) {
  final minutes = (seconds / 60).ceil();

  if (minutes <= 1) {
    return 'دقيقة';
  }

  if (minutes == 2) {
    return 'دقيقتان';
  }

  final count = arabicDigits('$minutes');

  return minutes <= 10 ? '$count دقائق' : '$count دقيقة';
}

/// "+٥٠٠ د.ع" for money that came in, "−٦٬٠٠٠ د.ع" for money that went out.
String formatSignedMoney(String amount, String currencyCode) {
  final value = double.tryParse(amount) ?? 0;
  final sign = value > 0 ? '+' : (value < 0 ? '−' : '');

  return '$sign${formatMoney(amount, currencyCode).replaceFirst('-', '')}';
}

const List<String> _monthNames = <String>[
  'يناير',
  'فبراير',
  'مارس',
  'أبريل',
  'مايو',
  'يونيو',
  'يوليو',
  'أغسطس',
  'سبتمبر',
  'أكتوبر',
  'نوفمبر',
  'ديسمبر',
];

/// "اليوم · ٤:١٢ م", "أمس · ٧:٤٥ م" or "١٥ سبتمبر · ٩:٠٠ ص", in the phone's time zone.
String formatTripDate(DateTime? moment) {
  if (moment == null) {
    return '';
  }

  final local = moment.toLocal();
  final now = DateTime.now();
  final today = DateTime(now.year, now.month, now.day);
  final day = DateTime(local.year, local.month, local.day);
  final daysAgo = today.difference(day).inDays;

  final hour12 = local.hour % 12 == 0 ? 12 : local.hour % 12;
  final minute = local.minute.toString().padLeft(2, '0');
  final period = local.hour < 12 ? 'ص' : 'م';
  final time = '${arabicDigits('$hour12:$minute')} $period';

  final String date;
  if (daysAgo == 0) {
    date = 'اليوم';
  } else if (daysAgo == 1) {
    date = 'أمس';
  } else {
    date = '${arabicDigits('${local.day}')} ${_monthNames[local.month - 1]}';
  }

  return '$date · $time';
}

/// A whole amount typed by a person ("20000", "٢٠٬٠٠٠", "20,000"), or null if it is not one.
int? parseAmount(String input) {
  final buffer = StringBuffer();

  for (final rune in input.runes) {
    if (rune >= 0x30 && rune <= 0x39) {
      buffer.writeCharCode(rune);
    } else if (rune >= 0x0660 && rune <= 0x0669) {
      buffer.writeCharCode(0x30 + (rune - 0x0660));
    } else if (rune >= 0x06F0 && rune <= 0x06F9) {
      buffer.writeCharCode(0x30 + (rune - 0x06F0));
    }
  }

  final digits = buffer.toString();
  if (digits.isEmpty || digits.length > 9) {
    return null;
  }

  return int.parse(digits);
}
