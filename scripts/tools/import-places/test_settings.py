"""python -m unittest discover -s scripts/tools/import-places (no database needed)."""
import json
import tempfile
import unittest

import import_places
import settings as s


class SettingsTest(unittest.TestCase):
    def setUp(self):
        self.defaults = s.load("")

    def test_an_instance_file_changes_only_what_it_sets(self):
        with tempfile.NamedTemporaryFile("w", suffix=".json") as file:
            json.dump({"min_confidence": 0.7, "_comment": "ignored"}, file)
            file.flush()
            loaded = s.load(file.name)

        self.assertEqual(loaded["min_confidence"], 0.7)
        self.assertEqual(loaded["dedupe_meters"], self.defaults["dedupe_meters"])
        self.assertNotIn("_comment", loaded)

    def test_a_missing_or_empty_file_means_the_defaults(self):
        self.assertEqual(s.load("/nowhere/places-import.json"), self.defaults)

        with tempfile.NamedTemporaryFile("w", suffix=".json") as file:
            self.assertEqual(s.load(file.name), self.defaults)

    def test_kinds_come_from_the_category_words(self):
        self.assertEqual(s.kind_of("shopping_mall", self.defaults), "mall")
        self.assertEqual(s.kind_of("college_university", self.defaults), "university")
        self.assertEqual(s.kind_of("coffee_shop", self.defaults), "restaurant")
        self.assertEqual(s.kind_of("monument", self.defaults), "landmark")
        self.assertEqual(s.kind_of("dental_clinic", self.defaults), "other")
        self.assertEqual(s.kind_of("", self.defaults), "other")

    def test_a_category_weight_overrides_its_kind(self):
        self.assertEqual(s.weight_of("real_estate_service", "other", self.defaults), 0.3)
        self.assertEqual(s.weight_of("shopping_mall", "mall", self.defaults), 2.0)

    def test_closed_weak_nameless_and_excluded_places_are_left_out(self):
        place = {"name": "Cafe", "confidence": 0.9, "status": "open", "category": "cafe"}
        self.assertTrue(s.keeps(place, self.defaults))
        self.assertFalse(s.keeps({**place, "status": "permanently_closed"}, self.defaults))
        self.assertFalse(s.keeps({**place, "confidence": 0.2}, self.defaults))
        self.assertFalse(s.keeps({**place, "name": "  "}, self.defaults))
        self.assertFalse(s.keeps(place, {**self.defaults, "exclude_categories": ["cafe"]}))


class PreparedTest(unittest.TestCase):
    def test_a_place_becomes_a_row_with_its_address_and_other_names(self):
        row = import_places.prepared({
            "id": "x" * 150, "name": " Family Mall ", "other_names": "فاميلي مول", "brand": "Family",
            "category": "shopping_mall", "confidence": 0.9, "status": "open",
            "street": "100m Street", "locality": "Erbil", "longitude": 43.98, "latitude": 36.23,
        }, s.load(""))

        self.assertEqual(len(row["id"]), 100)
        self.assertEqual(row["name"], "Family Mall")
        self.assertEqual(row["kind"], "mall")
        self.assertEqual(row["address"], "100m Street, Erbil")
        self.assertEqual(row["alt_names"], "فاميلي مول Family")


if __name__ == "__main__":
    unittest.main()
