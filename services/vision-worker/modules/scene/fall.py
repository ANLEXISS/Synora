class FallDetector:

    def detect(self, tracks):

        falls = []

        for t in tracks:

            x1,y1,x2,y2 = t.box

            w = x2-x1
            h = y2-y1

            if h <= 0:
                continue

            ratio = w/h

            if ratio > 1.3:

                falls.append({
                    "track": t.id
                })

        return falls
