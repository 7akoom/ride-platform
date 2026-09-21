import 'package:flutter/foundation.dart';

import 'api_exception.dart';

/// What to tell the rider (in Arabic) when a call failed.
///
/// [wrong] is the message for "the backend refused what you typed" (400 to 404), which
/// differs from screen to screen. In a debug build the technical reason is added under
/// it, so a problem can be reported without a console.
String describeFailure(ApiException error, {required String wrong}) {
  final String friendly;

  if (error.isNetwork) {
    friendly = 'تعذر الاتصال بالخادم. تأكد من الإنترنت وحاول مرة أخرى';
  } else if (error.isTooManyRequests) {
    friendly = 'محاولات كثيرة. انتظر قليلاً ثم حاول مرة أخرى';
  } else if (error.isServerError) {
    friendly = 'حدثت مشكلة عندنا. حاول مرة أخرى بعد قليل';
  } else if (error.statusCode != null && error.statusCode! >= 400 && error.statusCode! < 500) {
    friendly = wrong;
  } else {
    friendly = 'حدث خطأ غير متوقع. حاول مرة أخرى';
  }

  return kDebugMode ? '$friendly\n(${error.statusCode ?? 'no answer'}: ${error.message})' : friendly;
}
