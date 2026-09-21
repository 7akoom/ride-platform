import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_exception.dart';
import '../../core/api/api_messages.dart';
import '../../state/api_providers.dart';
import '../../state/session_storage.dart';
import '../../theme/app_theme.dart';
import '../onboarding/splash_screen.dart';
import '../onboarding/account_router.dart';

const _codeLength = 6;

class OtpScreen extends ConsumerStatefulWidget {
  /// What the person typed, for showing back to them.
  final String phoneNumber;

  /// The same number in international form (+9647XXXXXXXXX).
  final String e164Phone;

  /// Which code request this screen is checking the answer to.
  final String challengeId;

  const OtpScreen({
    super.key,
    required this.phoneNumber,
    required this.e164Phone,
    required this.challengeId,
  });

  @override
  ConsumerState<OtpScreen> createState() => _OtpScreenState();
}

class _OtpScreenState extends ConsumerState<OtpScreen> {
  final _controllers = List.generate(_codeLength, (_) => TextEditingController());
  final _focusNodes = List.generate(_codeLength, (_) => FocusNode());

  Timer? _timer;
  int _secondsLeft = 45;
  bool _verifying = false;
  String? _error;
  late String _challengeId;

  @override
  void initState() {
    super.initState();
    _challengeId = widget.challengeId;
    _startResendTimer();
  }

  void _startResendTimer() {
    _secondsLeft = 45;
    _timer?.cancel();
    _timer = Timer.periodic(const Duration(seconds: 1), (t) {
      if (_secondsLeft == 0) {
        t.cancel();
        return;
      }
      setState(() => _secondsLeft--);
    });
  }

  String get _code => _controllers.map((c) => c.text).join();

  Future<void> _verify() async {
    if (_code.length != _codeLength || _verifying) return;
    setState(() {
      _verifying = true;
      _error = null;
    });

    final AuthTokensResult login = await _login();
    if (!mounted) return;

    if (login.error != null) {
      setState(() {
        _verifying = false;
        _error = login.error;
      });
      _clearCode();
      return;
    }

    // Signed in. Now find out where this driver's account stands.
    DriverAccountState account;
    try {
      account = await ref.read(accountServiceProvider).resolve();
    } on ApiException {
      // The login worked but the connection dropped: the opening screen checks again and
      // offers a retry, so nothing typed here is lost.
      if (!mounted) return;
      Navigator.of(context).pushAndRemoveUntil(
        MaterialPageRoute(builder: (_) => const SplashScreen()),
        (route) => false,
      );
      return;
    }

    if (!mounted) return;
    setState(() => _verifying = false);

    Navigator.of(context).pushAndRemoveUntil(
      MaterialPageRoute(builder: (_) => screenForAccount(account)),
      (route) => false,
    );
  }

  /// Checks the code with the backend and stores the tokens. Returns the message to show
  /// when it did not work.
  Future<AuthTokensResult> _login() async {
    try {
      final tokens = await ref.read(authApiProvider).verifyOtp(
            challengeId: _challengeId,
            code: _code,
          );

      if (tokens.accessToken.isEmpty || tokens.refreshToken.isEmpty) {
        return const AuthTokensResult(error: 'تعذر تسجيل الدخول. حاول مرة أخرى');
      }

      await SessionStorage.saveTokens(
        accessToken: tokens.accessToken,
        refreshToken: tokens.refreshToken,
        identityId: tokens.identityId,
      );

      return const AuthTokensResult();
    } on ApiException catch (error) {
      return AuthTokensResult(
        error: describeFailure(error, wrong: 'الرمز غير صحيح أو انتهت صلاحيته'),
      );
    }
  }

  void _clearCode() {
    for (final controller in _controllers) {
      controller.clear();
    }
    _focusNodes.first.requestFocus();
  }

  /// Asks for a new code. The old one stops working, so the screen follows the new request.
  Future<void> _resend() async {
    setState(() => _error = null);

    try {
      final challenge = await ref.read(authApiProvider).requestOtp(widget.e164Phone);
      if (!mounted) return;

      _challengeId = challenge.challengeId;
      _clearCode();
      _startResendTimer();
    } on ApiException catch (error) {
      if (!mounted) return;
      setState(() {
        _error = describeFailure(error, wrong: 'تعذر إرسال الرمز. حاول مرة أخرى');
      });
    }
  }

  @override
  void dispose() {
    _timer?.cancel();
    for (final c in _controllers) {
      c.dispose();
    }
    for (final f in _focusNodes) {
      f.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;

    return Scaffold(
      backgroundColor: colors.surface200,
      appBar: AppBar(
        backgroundColor: colors.surface200,
        elevation: 0,
        leading: IconButton(
          icon: const Icon(Icons.arrow_back_ios_new, size: 18),
          onPressed: () => Navigator.of(context).maybePop(),
        ),
      ),
      body: Padding(
        padding: const EdgeInsets.symmetric(horizontal: AppSpacing.space5),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text('أدخل رمز التحقق', style: textTheme.titleLarge),
            const SizedBox(height: AppSpacing.space1),
            Text(
              'أرسلنا رمزاً مكوّناً من ٦ أرقام إلى +٩٦٤ ${widget.phoneNumber}',
              style: textTheme.bodyLarge?.copyWith(
                color: colors.inkMuted,
                fontSize: 14,
              ),
            ),
            const SizedBox(height: AppSpacing.space6),
            Directionality(
              textDirection: TextDirection.ltr,
              child: Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: List.generate(_codeLength, (i) {
                  return SizedBox(
                    width: 44,
                    height: 52,
                    child: TextField(
                      controller: _controllers[i],
                      focusNode: _focusNodes[i],
                      textAlign: TextAlign.center,
                      keyboardType: TextInputType.number,
                      maxLength: 1,
                      style: textTheme.titleLarge,
                      inputFormatters: [
                        FilteringTextInputFormatter.digitsOnly,
                      ],
                      decoration: InputDecoration(
                        counterText: '',
                        contentPadding: EdgeInsets.zero,
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
                      onChanged: (value) {
                        if (value.isNotEmpty && i < _codeLength - 1) {
                          _focusNodes[i + 1].requestFocus();
                        }
                        if (value.isEmpty && i > 0) {
                          _focusNodes[i - 1].requestFocus();
                        }
                        setState(() {});
                        if (_code.length == _codeLength) _verify();
                      },
                    ),
                  );
                }),
              ),
            ),
            if (_error != null) ...[
              const SizedBox(height: AppSpacing.space3),
              Text(
                _error!,
                style: textTheme.bodySmall?.copyWith(color: colors.danger),
              ),
            ],
            const SizedBox(height: AppSpacing.space5),
            Center(
              child: _secondsLeft > 0
                  ? Text(
                      'إعادة الإرسال بعد ٠٠:${_secondsLeft.toString().padLeft(2, '0')}',
                      style: textTheme.bodySmall?.copyWith(
                        color: colors.inkMuted,
                      ),
                    )
                  : TextButton(
                      onPressed: _resend,
                      child: const Text('إعادة إرسال الرمز'),
                    ),
            ),
            const SizedBox(height: AppSpacing.space5),
            ElevatedButton(
              onPressed: _code.length == _codeLength && !_verifying
                  ? _verify
                  : null,
              style: ElevatedButton.styleFrom(
                disabledBackgroundColor: colors.inkMuted,
              ),
              child: _verifying
                  ? const SizedBox(
                      width: 20,
                      height: 20,
                      child: CircularProgressIndicator(
                        strokeWidth: 2,
                        color: Colors.white,
                      ),
                    )
                  : const Text('تحقق'),
            ),
          ],
        ),
      ),
    );
  }
}

/// The outcome of the login step: an error message to show, or nothing if it worked.
class AuthTokensResult {
  final String? error;

  const AuthTokensResult({this.error});
}
