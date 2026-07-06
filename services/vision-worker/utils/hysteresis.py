import time


class Hysteresis:
    def __init__(
        self,
        activation_threshold: float,
        deactivation_threshold: float,
        activation_delay: float,
        deactivation_delay: float,
    ):
        self.activation_threshold = activation_threshold
        self.deactivation_threshold = deactivation_threshold
        self.activation_delay = activation_delay
        self.deactivation_delay = deactivation_delay

        self._state = False
        self._activation_start = None
        self._deactivation_start = None

    @property
    def state(self) -> bool:
        return self._state

    def update(self, score: float) -> bool:
        now = time.time()

        if not self._state:
            if score > self.activation_threshold:
                if self._activation_start is None:
                    self._activation_start = now
                elif now - self._activation_start > self.activation_delay:
                    self._state = True
                    self._activation_start = None
            else:
                self._activation_start = None

        else:
            if score < self.deactivation_threshold:
                if self._deactivation_start is None:
                    self._deactivation_start = now
                elif now - self._deactivation_start > self.deactivation_delay:
                    self._state = False
                    self._deactivation_start = None
            else:
                self._deactivation_start = None

        return self._state
