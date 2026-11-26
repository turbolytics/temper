#!/bin/sh
# Django's ORM test modules against PostgreSQL (the backend, query, lookup,
# expression, aggregation and contrib.postgres suites), at a pinned release.
set -e
VERSION=6.1.3
git clone -q --depth 1 -b "$VERSION" https://github.com/django/django /work/django
cd /work/django
pip install -q -e . "psycopg[binary]==3.3.9"
pip install -q -r tests/requirements/py3.txt 2>/dev/null || true
cat > tests/test_capture.py <<PY
DATABASES = {
    "default": {"ENGINE": "django.db.backends.postgresql", "NAME": "django", "USER": "postgres",
                "PASSWORD": "postgres", "HOST": "capture"},
    "other": {"ENGINE": "django.db.backends.postgresql", "NAME": "django_other", "USER": "postgres",
              "PASSWORD": "postgres", "HOST": "capture"},
}
SECRET_KEY = "capture"
PASSWORD_HASHERS = ["django.contrib.auth.hashers.MD5PasswordHasher"]
DEFAULT_AUTO_FIELD = "django.db.models.AutoField"
USE_TZ = True
PY
cd tests
python runtests.py --settings=test_capture --parallel=1 --noinput -v 0 \
  queries aggregation aggregation_regress annotations expressions expressions_window lookup \
  ordering model_fields db_functions custom_lookups select_related prefetch_related \
  many_to_one many_to_many update delete bulk_create get_or_create transactions \
  backends postgres_tests || true
echo "django $VERSION"
