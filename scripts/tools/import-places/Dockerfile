# The place import tool (import_places.py), built and run on a server by
# scripts/deploy/import-places.sh.
FROM python:3.12-slim
RUN useradd --create-home importer
WORKDIR /tool
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
USER importer
# DuckDB's S3 reader, installed once here instead of on every run.
RUN python -c "import duckdb; duckdb.connect().sql('INSTALL httpfs')"
COPY defaults.json *.py ./
ENTRYPOINT ["python", "import_places.py"]
