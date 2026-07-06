# core/state.py

from dataclasses import dataclass
from typing import Optional
import time


@dataclass
class VisionState:
    person_score: float = 0.0
    person_present: bool = False
    face_detected: bool = False
    identity: Optional[str] = None
    timestamp: float = 0.0

    def update_timestamp(self):
        self.timestamp = time.time()
