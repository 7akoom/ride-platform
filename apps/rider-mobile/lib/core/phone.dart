/// Turns what a person typed into an Iraqi mobile number in international form
/// (+9647XXXXXXXXX), or null if it cannot be one.
///
/// Accepts Arabic-Indic and Persian digits, spaces, dashes and brackets, a leading
/// 0 (07701234567), a leading 964 or +964.
String? normalizeIraqPhone(String input) {
  final buffer = StringBuffer();

  for (final rune in input.runes) {
    if (rune >= 0x30 && rune <= 0x39) {
      buffer.writeCharCode(rune); // 0-9
    } else if (rune >= 0x0660 && rune <= 0x0669) {
      buffer.writeCharCode(0x30 + (rune - 0x0660)); // ٠-٩
    } else if (rune >= 0x06F0 && rune <= 0x06F9) {
      buffer.writeCharCode(0x30 + (rune - 0x06F0)); // ۰-۹
    }
  }

  var digits = buffer.toString();

  if (digits.startsWith('00964')) {
    digits = digits.substring(5);
  } else if (digits.startsWith('964')) {
    digits = digits.substring(3);
  }

  if (digits.startsWith('0')) {
    digits = digits.substring(1);
  }

  // Iraqi mobile numbers are 7XXXXXXXXX: ten digits, starting with 7.
  if (digits.length != 10 || !digits.startsWith('7')) {
    return null;
  }

  return '+964$digits';
}
