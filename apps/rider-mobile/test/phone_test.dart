import 'package:flutter_test/flutter_test.dart';

import 'package:rider_app/core/phone.dart';

void main() {
  group('normalizeIraqPhone', () {
    test('accepts the usual ways of writing an Iraqi mobile number', () {
      const expected = '+9647701234567';

      expect(normalizeIraqPhone('7701234567'), expected);
      expect(normalizeIraqPhone('07701234567'), expected);
      expect(normalizeIraqPhone('+964 770 123 4567'), expected);
      expect(normalizeIraqPhone('00964-770-123-4567'), expected);
      expect(normalizeIraqPhone('964 770 123 4567'), expected);
      expect(normalizeIraqPhone('(770) 123-4567'), expected);
    });

    test('accepts Arabic-Indic and Persian digits', () {
      expect(normalizeIraqPhone('٠٧٧٠١٢٣٤٥٦٧'), '+9647701234567');
      expect(normalizeIraqPhone('۰۷۷۰۱۲۳۴۵۶۷'), '+9647701234567');
      expect(normalizeIraqPhone('٧٧٠ ١٢٣ ٤٥٦٧'), '+9647701234567');
    });

    test('refuses what cannot be an Iraqi mobile number', () {
      expect(normalizeIraqPhone(''), isNull);
      expect(normalizeIraqPhone('770123456'), isNull); // one digit short
      expect(normalizeIraqPhone('77012345678'), isNull); // one digit too many
      expect(normalizeIraqPhone('5701234567'), isNull); // does not start with 7
      expect(normalizeIraqPhone('abc'), isNull);
    });
  });
}
