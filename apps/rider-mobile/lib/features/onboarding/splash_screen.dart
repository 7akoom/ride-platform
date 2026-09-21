import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_exception.dart';
import '../../state/api_providers.dart';
import '../../state/app_prefs.dart';
import '../auth/phone_entry_screen.dart';
import '../auth/profile_setup_screen.dart';
import '../rider/request_ride_screen.dart';
import 'language_select_screen.dart';

class SplashScreen extends ConsumerStatefulWidget {
  const SplashScreen({super.key});

  @override
  ConsumerState<SplashScreen> createState() => _SplashScreenState();
}

class _SplashScreenState extends ConsumerState<SplashScreen> with SingleTickerProviderStateMixin {
  late final AnimationController _controller;

  /// True when the backend could not be reached while checking the stored login.
  bool _offline = false;

  static const _dots = [
    (Offset(10, 90), 7.0, 0.05),
    (Offset(45, 55), 6.0, 0.20),
    (Offset(80, 48), 6.0, 0.35),
    (Offset(115, 58), 7.0, 0.50),
    (Offset(150, 46), 6.0, 0.65),
    (Offset(185, 34), 6.0, 0.80),
    (Offset(210, 30), 9.0, 0.95),
  ];

  @override
  void initState() {
    super.initState();
    _controller = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 1600),
    )..forward();
    _decideNext();
  }

  Future<void> _decideNext() async {
    await Future.delayed(const Duration(milliseconds: 1700));
    if (!mounted) return;

    final onboarded = await AppPrefs.hasOnboarded();
    if (!mounted) return;

    if (!onboarded) {
      Navigator.of(context).pushReplacement(
        MaterialPageRoute(builder: (_) => const LanguageSelectScreen()),
      );
      return;
    }

    await _routeByAccount();
  }

  /// A stored login is only trusted after the backend has confirmed it (renewing the
  /// access token on the way if it ran out).
  Future<void> _routeByAccount() async {
    if (_offline) setState(() => _offline = false);

    try {
      final account = await ref.read(accountServiceProvider).resolve();
      if (!mounted) return;

      final Widget next = switch (account) {
        AccountState.ready => const RequestRideScreen(),
        AccountState.needsProfile => const ProfileSetupScreen(),
        AccountState.signedOut => const PhoneEntryScreen(),
      };

      Navigator.of(context).pushReplacement(MaterialPageRoute(builder: (_) => next));
    } on ApiException catch (error) {
      if (!mounted) return;

      // No answer from the backend: the login may be fine, so keep it and let the person retry.
      if (error.isNetwork || error.isServerError) {
        setState(() => _offline = true);
        return;
      }

      Navigator.of(context).pushReplacement(
        MaterialPageRoute(builder: (_) => const PhoneEntryScreen()),
      );
    }
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: const Color(0xFF0E7C7B),
      body: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            SizedBox(
              width: 220,
              height: 120,
              child: AnimatedBuilder(
                animation: _controller,
                builder: (context, child) {
                  return CustomPaint(
                    painter: _RoutePainter(_controller.value, _dots),
                  );
                },
              ),
            ),
            const SizedBox(height: 28),
            AnimatedOpacity(
              opacity: _controller.value > 0.65 ? 1 : 0,
              duration: const Duration(milliseconds: 400),
              child: Column(
                children: const [
                  Text(
                    'Ride Platform',
                    style: TextStyle(
                      fontSize: 28,
                      fontWeight: FontWeight.w600,
                      color: Colors.white,
                    ),
                  ),
                  SizedBox(height: 6),
                  Text(
                    'نقلك يبدأ من هنا',
                    style: TextStyle(fontSize: 13, color: Color(0xCCFFFFFF)),
                  ),
                ],
              ),
            ),
            if (_offline) ...[
              const SizedBox(height: 28),
              const Text(
                'تعذر الاتصال بالخادم',
                style: TextStyle(fontSize: 14, color: Colors.white),
              ),
              TextButton(
                onPressed: _routeByAccount,
                child: const Text(
                  'إعادة المحاولة',
                  style: TextStyle(color: Colors.white, fontWeight: FontWeight.w600),
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _RoutePainter extends CustomPainter {
  final double t;
  final List<(Offset, double, double)> dots;

  _RoutePainter(this.t, this.dots);

  @override
  void paint(Canvas canvas, Size size) {
    final linePaint = Paint()
      ..color = Colors.white.withOpacity(0.25)
      ..strokeWidth = 2
      ..style = PaintingStyle.stroke;
    final path = Path()..moveTo(10, 90);
    path.quadraticBezierTo(60, 20, 110, 60);
    path.quadraticBezierTo(160, 90, 210, 30);
    canvas.drawPath(path, linePaint);

    for (final (offset, radius, delay) in dots) {
      final localT = ((t - delay) / (1 - delay)).clamp(0.0, 1.0);
      if (localT <= 0) continue;
      final scale = Curves.easeOut.transform(localT);
      final dotPaint = Paint()..color = Colors.white.withOpacity(scale);
      canvas.drawCircle(offset, radius * scale, dotPaint);
    }
  }

  @override
  bool shouldRepaint(covariant _RoutePainter oldDelegate) => oldDelegate.t != t;
}
