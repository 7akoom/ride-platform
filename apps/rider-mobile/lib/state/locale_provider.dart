import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

/// Only ar/en are real Flutter localizations here; 'ku' (Sorani) has no
/// GlobalMaterialLocalizations delegate, so picking it only affects the
/// stored preference below, not system widgets (date pickers, etc.) — see
/// the TODO on SettingsScreen for what full Kurdish support still needs.
final localeProvider = StateProvider<Locale>((ref) => const Locale('ar'));
