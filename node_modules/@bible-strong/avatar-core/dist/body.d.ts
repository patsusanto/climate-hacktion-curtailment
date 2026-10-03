import { type SurfaceConfig } from './surfaces';
export type BodyVector = readonly [number, number, number];
export type BodyNode = {
    id: string;
    name: string;
    surface: SurfaceConfig;
    position: BodyVector;
    rotation: BodyVector;
};
export type AvatarBody = {
    primary: SurfaceConfig;
    nodes: BodyNode[];
};
export declare const bodyPrimitiveTypes: readonly ["sphere", "cube", "capsule", "cylinder", "cone", "diamond"];
export declare const MAX_BODY_NODES = 16;
export declare const parseSurfaceConfig: (value: unknown, fallback: SurfaceConfig) => SurfaceConfig;
export declare const parseAvatarBody: (value: unknown, fallbackPrimary: SurfaceConfig) => AvatarBody;
export declare const createBodyNode: (type: (typeof bodyPrimitiveTypes)[number], index: number) => BodyNode;
export declare const duplicateBodyNode: (source: BodyNode) => BodyNode;
//# sourceMappingURL=body.d.ts.map