import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_exception.dart';
import '../../core/api/api_messages.dart';
import '../../state/api_providers.dart';
import '../../theme/app_theme.dart';
import '../onboarding/account_router.dart';

/// Shown while the account cannot work yet: waiting for the operator's approval, or
/// refused, or suspended. While it is pending the screen checks every 15 seconds and moves
/// on by itself when the account is approved.
class AccountStatusScreen extends ConsumerStatefulWidget {
  final DriverAccountState state;

  const AccountStatusScreen({super.key, required this.state});

  @override
  ConsumerState<AccountStatusScreen> createState() => _AccountStatusScreenState();
}

class _AccountStatusScreenState extends ConsumerState<AccountStatusScreen> {
  late DriverAccountState _state;
  Timer? _timer;
  bool _checking = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _state = widget.state;
    _timer = Timer.periodic(const Duration(seconds: 15), (_) => _check(silent: true));
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  Future<void> _check({bool silent = false}) async {
    if (_checking) return;

    setState(() {
      _checking = true;
      if (!silent) _error = null;
    });

    try {
      final account = await ref.read(accountServiceProvider).resolve();
      if (!mounted) return;

      if (account == DriverAccountState.ready || account == DriverAccountState.signedOut) {
        _timer?.cancel();
        Navigator.of(context).pushAndRemoveUntil(
          MaterialPageRoute(builder: (_) => screenForAccount(account)),
          (route) => false,
        );
        return;
      }

      setState(() {
        _state = account;
        _checking = false;
      });
    } on ApiException catch (error) {
      if (!mounted) return;
      setState(() {
        _checking = false;
        if (!silent) {
          _error = describeFailure(error, wrong: 'تعذر تحديث الحالة. حاول مرة أخرى');
        }
      });
    }
  }

  String get _title {
    switch (_state) {
      case DriverAccountState.rejected:
        return 'تعذّرت الموافقة على طلبك';
      case DriverAccountState.suspended:
        return 'حسابك موقوف';
      case DriverAccountState.pending:
      case DriverAccountState.needsRegistration:
      case DriverAccountState.ready:
      case DriverAccountState.signedOut:
        return 'طلبك قيد المراجعة';
    }
  }

  String get _body {
    switch (_state) {
      case DriverAccountState.rejected:
        return 'للاستفسار عن السبب تواصل مع الشركة المشغّلة.';
      case DriverAccountState.suspended:
        return 'تواصل مع الشركة المشغّلة لمعرفة السبب وإعادة تفعيل حسابك.';
      case DriverAccountState.pending:
      case DriverAccountState.needsRegistration:
      case DriverAccountState.ready:
      case DriverAccountState.signedOut:
        return 'نراجع بياناتك ومركبتك. أول ما نفعّل حسابك تقدر تبدأ باستقبال الرحلات، وهالشاشة تتحدث لحالها.';
    }
  }

  IconData get _icon {
    switch (_state) {
      case DriverAccountState.rejected:
        return Icons.block;
      case DriverAccountState.suspended:
        return Icons.pause_circle_outline;
      case DriverAccountState.pending:
      case DriverAccountState.needsRegistration:
      case DriverAccountState.ready:
      case DriverAccountState.signedOut:
        return Icons.hourglass_top;
    }
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final textTheme = Theme.of(context).textTheme;
    final bad = _state == DriverAccountState.rejected || _state == DriverAccountState.suspended;

    return Scaffold(
      backgroundColor: colors.surface200,
      body: SafeArea(
        child: Padding(
          padding: const EdgeInsets.all(AppSpacing.space5),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const Spacer(),
              Center(
                child: Container(
                  width: 84,
                  height: 84,
                  decoration: BoxDecoration(color: bad ? colors.surface100 : colors.brand100, shape: BoxShape.circle),
                  child: Icon(_icon, size: 38, color: bad ? colors.danger : colors.brand600),
                ),
              ),
              const SizedBox(height: AppSpacing.space5),
              Text(_title, textAlign: TextAlign.center, style: textTheme.titleLarge),
              const SizedBox(height: AppSpacing.space2),
              Text(
                _body,
                textAlign: TextAlign.center,
                style: textTheme.bodyLarge?.copyWith(color: colors.inkMuted, fontSize: 14),
              ),
              if (_error != null) ...[
                const SizedBox(height: AppSpacing.space3),
                Text(_error!, textAlign: TextAlign.center, style: textTheme.bodySmall?.copyWith(color: colors.danger)),
              ],
              const Spacer(),
              ElevatedButton(
                onPressed: _checking ? null : _check,
                child: _checking
                    ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                    : const Text('تحديث الحالة'),
              ),
              const SizedBox(height: AppSpacing.space2),
              TextButton(
                onPressed: () => signOut(ref),
                child: const Text('تسجيل الخروج'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
