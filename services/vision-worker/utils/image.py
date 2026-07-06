import cv2
import numpy as np


ARC_FACE_TEMPLATE = np.array([
    [38.2946, 51.6963],
    [73.5318, 51.5014],
    [56.0252, 71.7366],
    [41.5493, 92.3655],
    [70.7299, 92.2041]
], dtype=np.float32)


def align_face(image, landmarks, size=112):

    try:

        src = np.array([
            landmarks[0],
            landmarks[1],
            landmarks[2],
            landmarks[3],
            landmarks[4]
        ], dtype=np.float32)

        dst = ARC_FACE_TEMPLATE

        M, _ = cv2.estimateAffinePartial2D(src, dst)

        if M is None:
            return None

        aligned = cv2.warpAffine(
            image,
            M,
            (size, size),
            flags=cv2.INTER_LINEAR
        )

        return aligned

    except Exception:
        return None
