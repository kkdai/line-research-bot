import os
import uuid
import pytest
from google.cloud import firestore


@pytest.fixture
def firestore_client() -> firestore.Client:
    """Yields a Firestore client pointed at the local emulator.
    Each test gets a unique project id so collections don't collide.
    """
    project = f"test-{uuid.uuid4().hex[:8]}"
    os.environ["FIRESTORE_EMULATOR_HOST"] = os.environ.get(
        "FIRESTORE_EMULATOR_HOST", "localhost:8081"
    )
    return firestore.Client(project=project)
