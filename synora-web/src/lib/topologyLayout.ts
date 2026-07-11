type LayoutRoom = {
  id: string;
  name: string;
  floorId: string;
  connect: string[];
  dynamic_score: number;
};

type PositionedRoom = LayoutRoom & {
  col: number;
  row: number;
};

function roomRoleScore(name: string) {
  const key = name.toLowerCase();

  if (key.includes("couloir")) return 100;
  if (key.includes("entree")) return 90;
  if (key.includes("salon")) return 80;
  if (key.includes("salle_a_manger")) return 70;
  if (key.includes("cuisine")) return 60;
  if (key.includes("bureau")) return 50;
  if (key.includes("chambre")) return 40;
  if (key.includes("bain")) return 30;

  return 10;
}

function sameFloorConnections(room: LayoutRoom, rooms: LayoutRoom[]) {
  const roomIds = new Set(rooms.map((r) => r.id));
  return room.connect.filter((id) => roomIds.has(id));
}

function chooseAnchorRoom(rooms: LayoutRoom[]) {
  const couloir = rooms.find((r) => r.name.toLowerCase().includes("couloir"));
  if (couloir) return couloir;

  return [...rooms].sort((a, b) => {
    const degA = sameFloorConnections(a, rooms).length;
    const degB = sameFloorConnections(b, rooms).length;

    if (degB !== degA) return degB - degA;
    return roomRoleScore(b.name) - roomRoleScore(a.name);
  })[0];
}

export function buildFloorLayout(rooms: LayoutRoom[]): PositionedRoom[] {
  if (rooms.length === 0) return [];

  const byId = new Map(rooms.map((r) => [r.id, r]));
  const anchor = chooseAnchorRoom(rooms);

  // BFS distances
  const distance = new Map<string, number>();
  const queue: string[] = [anchor.id];
  distance.set(anchor.id, 0);

  while (queue.length > 0) {
    const currentId = queue.shift()!;
    const current = byId.get(currentId);
    if (!current) continue;

    const neighbors = sameFloorConnections(current, rooms);
    for (const nextId of neighbors) {
      if (!distance.has(nextId)) {
        distance.set(nextId, (distance.get(currentId) ?? 0) + 1);
        queue.push(nextId);
      }
    }
  }

  // pièces non atteintes (si topo partielle)
  for (const room of rooms) {
    if (!distance.has(room.id)) {
      distance.set(room.id, 999);
    }
  }

  // group by distance / column
  const groups = new Map<number, LayoutRoom[]>();
  for (const room of rooms) {
    const col = distance.get(room.id)!;
    if (!groups.has(col)) groups.set(col, []);
    groups.get(col)!.push(room);
  }

  const sortedCols = [...groups.keys()].sort((a, b) => a - b);

  const positioned: PositionedRoom[] = [];

  for (const col of sortedCols) {
    const group = groups.get(col)!;

    group.sort((a, b) => {
      const connDiff =
        sameFloorConnections(b, rooms).length - sameFloorConnections(a, rooms).length;
      if (connDiff !== 0) return connDiff;

      return roomRoleScore(b.name) - roomRoleScore(a.name);
    });

    group.forEach((room, index) => {
      positioned.push({
        ...room,
        col,
        row: index,
      });
    });
  }

  return positioned;
}
