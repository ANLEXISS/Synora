#!/usr/bin/env bash

WORKER_URL="http://localhost:7000/vision"

KNOWN_IMAGE="/var/home/bazzite/Orion/services/vision-worker/dev/images/face.txt"
UNKNOWN_IMAGE="/var/home/bazzite/Orion/services/vision-worker/dev/images/false.txt"

echo "--------------------------------"
echo "TEST 1 : Visage reconnu"
echo "--------------------------------"

curl -s -X POST $WORKER_URL \
-H "Content-Type: application/json" \
-d "{\"camera\":\"cam_01\",\"image\":\"$(cat $KNOWN_IMAGE)\"}"

sleep 1

VISION_LOG=$(journalctl -u orion-vision -n 5 --no-pager | tail -n 1)
BUS_LOG=$(journalctl -u orion-bus -n 5 --no-pager | tail -n 1)
CORE_LOG=$(journalctl -u orion-core -n 5 --no-pager | tail -n 1)

echo
echo "VISION LOG:"
echo "$VISION_LOG"

echo
echo "BUS LOG:"
echo "$BUS_LOG"

echo
echo "CORE LOG:"
echo "$CORE_LOG"

echo
echo "--------------------------------"
echo "TEST 2 : Visage inconnu"
echo "--------------------------------"

curl -s -X POST $WORKER_URL \
-H "Content-Type: application/json" \
-d "{\"camera\":\"cam_01\",\"image\":\"$(cat $UNKNOWN_IMAGE)\"}"

sleep 1

VISION_LOG=$(journalctl -u orion-vision -n 5 --no-pager | tail -n 1)
BUS_LOG=$(journalctl -u orion-bus -n 5 --no-pager | tail -n 1)
CORE_LOG=$(journalctl -u orion-core -n 5 --no-pager | tail -n 1)

HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
-X POST $WORKER_URL \
-H "Content-Type: application/json" \
-d "{\"camera\":\"cam_01\",\"image\":\"$(cat $KNOWN_IMAGE)\"}")

echo
echo "VISION LOG:"
if [ "$HTTP_CODE" = "200" ]; then
    echo "✔ worker HTTP OK"
else
    echo "✖ worker HTTP FAIL"
fi

echo
echo "BUS LOG:"
if echo "$BUS_LOG" | grep -q "vision.identity"; then
    echo "✔ identity event reçu par le bus"
else
    echo "✖ identity event manquant"
fi

echo
echo "CORE LOG:"
if echo "$CORE_LOG" | grep -q "vision.identity"; then
    echo "✔ identity event reçu par le bus"
else
    echo "✖ identity event manquant"
fi

echo
echo "--------------------------------"
echo "TEST TERMINÉ"
echo "--------------------------------"
