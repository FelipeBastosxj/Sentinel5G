import numpy as np

from scripts.build_real_dataset import _REAL_DATASET_DIR, build
from sentinel_ai.features import FEATURE_VECTOR_SIZE


def test_build_real_dataset_produces_well_formed_feature_arrays():
    normal, anomalous = build(_REAL_DATASET_DIR)

    assert normal.shape[1] == FEATURE_VECTOR_SIZE
    assert anomalous.shape[1] == FEATURE_VECTOR_SIZE
    assert len(normal) > 0
    assert len(anomalous) > 0
    assert np.isfinite(normal).all()
    assert np.isfinite(anomalous).all()
