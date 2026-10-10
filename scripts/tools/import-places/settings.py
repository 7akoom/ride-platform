"""The import's settings: defaults.json, then the instance's own file on top."""
import json
import pathlib

DEFAULTS = pathlib.Path(__file__).with_name("defaults.json")


def load(path):
    """Defaults, with the keys the instance file sets replacing them. A missing or
    empty instance file means the defaults."""
    settings = _read(DEFAULTS)

    if path:
        file = pathlib.Path(path)
        if file.is_file() and file.read_text().strip():
            for key, value in _read(file).items():
                if not key.startswith("_"):
                    settings[key] = value

    return {key: value for key, value in settings.items() if not key.startswith("_")}


def _read(file):
    try:
        data = json.loads(file.read_text())
    except json.JSONDecodeError as error:
        raise SystemExit(f"{file}: not valid JSON ({error})")

    if not isinstance(data, dict):
        raise SystemExit(f"{file}: expected a JSON object")

    return data


def kind_of(category, settings):
    """The apps' kind for the dataset's category: the first kind one of whose
    words is in it; 'other' when none is."""
    text = (category or "").lower()

    for kind, words in settings["kinds"]:
        if any(word in text for word in words):
            return kind

    return "other"


def weight_of(category, kind, settings):
    weights = settings["category_weights"]

    if category in weights:
        return float(weights[category])

    return float(settings["kind_weights"].get(kind, 1.0))


def keeps(place, settings):
    """Whether a place from the dataset is worth searching for."""
    return (
        bool(place["name"].strip())
        and place["confidence"] >= settings["min_confidence"]
        and place["status"] not in settings["skip_statuses"]
        and place["category"] not in settings["exclude_categories"]
    )
