import cv2


class ClipReader:

    def __init__(self, sample_rate=5):

        self.sample_rate = sample_rate

    def read(self, path):

        cap = cv2.VideoCapture(path)

        frames = []
        idx = 0

        try:
            while True:

                ret, frame = cap.read()

                if not ret:
                    break

                if idx % self.sample_rate == 0:
                    frames.append(frame)

                idx += 1

        finally:
            cap.release()

        return frames
