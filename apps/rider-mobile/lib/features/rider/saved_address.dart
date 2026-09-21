import 'package:flutter/material.dart';

enum SavedAddressKind { home, work, other }

class SavedAddress {
  final String id;
  final String label;
  final String address;
  final SavedAddressKind kind;

  const SavedAddress({
    required this.id,
    required this.label,
    required this.address,
    required this.kind,
  });

  IconData get icon => switch (kind) {
        SavedAddressKind.home => Icons.home_outlined,
        SavedAddressKind.work => Icons.work_outline,
        SavedAddressKind.other => Icons.place_outlined,
      };
}

// TODO(robert): backed by a real per-rider "saved places" table once one
// exists — Rider service has nothing like this today, so this is local/
// in-memory only and resets every app launch.
final List<SavedAddress> demoSavedAddresses = [
  const SavedAddress(id: 'home', label: 'المنزل', address: 'حي برايتي، أربيل', kind: SavedAddressKind.home),
  const SavedAddress(id: 'work', label: 'العمل', address: 'المنطقة الصناعية، أربيل', kind: SavedAddressKind.work),
  const SavedAddress(id: 'grandma', label: 'بيت جدتي', address: 'عنكاوا، أربيل', kind: SavedAddressKind.other),
];
