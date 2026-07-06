class TrackContext:

    def __init__(self, track_id):

        self.id = track_id

        self.best_score = 0
        self.best_face = None

        self.golden_frame = None
        self.golden_bbox = None

        self.identity = None
        self.identity_score = 0

        self.weapon_detected = False
        self.weapon_confidence = 0

        self.fall_detected = False
        self.fight_detected = False

        # self.locked = False

        self.boost_frames = 2

        self.candidates = []
        self.last_candidate_time = 0.0
        self.last_bbox = None
        self.face_detected = False

        # ---- FALL DETECTION STATE ----
        self.prev_ratio = None
        self.prev_center_y = None
        self.fall_frames = 0
