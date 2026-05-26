from pathlib import Path

import yaml

from api_coverage import COVERED_ENDPOINTS


SPEC_FILES = {
    "data-model": "data-model.yaml",
    "ingestion": "ingestion.yaml",
    "decision-engine": "decision-engine.yaml",
}


def test_all_openapi_endpoints_have_integration_test_coverage():
    """Verify every documented OpenAPI operation is represented in the integration coverage registry."""
    root = Path(__file__).resolve().parent
    failures = []
    for service, filename in SPEC_FILES.items():
        spec = yaml.safe_load((root / filename).read_text())
        documented = set()
        for path, operations in spec["paths"].items():
            for method in operations:
                if method.lower() in {"get", "post", "put", "patch", "delete"}:
                    documented.add((method.upper(), path))
        missing = documented - COVERED_ENDPOINTS.get(service, set())
        stale = COVERED_ENDPOINTS.get(service, set()) - documented
        if missing:
            failures.append(f"{service} missing coverage: {sorted(missing)}")
        if stale:
            failures.append(f"{service} stale coverage entries: {sorted(stale)}")
    assert not failures, "\n".join(failures)
