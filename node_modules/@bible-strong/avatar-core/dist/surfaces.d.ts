import type { Point3 } from './geometry';
export type SurfaceType = 'sphere' | 'mickey' | 'cursor' | 'cube' | 'capsule' | 'cylinder' | 'cone' | 'diamond';
export type SurfaceConfig = {
    type: SurfaceType;
    width: number;
    height: number;
    depth: number;
    roundness: number;
    morphRoundness?: number;
    tipRoundness?: number;
    baseRoundness?: number;
};
export type SurfaceSample = {
    point: Point3;
    normal: Point3;
};
export declare const surfacePresets: Record<SurfaceType, SurfaceConfig>;
export declare const surfaceLabels: Record<SurfaceType, string>;
export declare const cursorLayout: (config: SurfaceConfig) => {
    coneApexY: number;
    coneBaseY: number;
    bodyHeight: number;
    bodyCenterY: number;
    bodyWidth: number;
    bodyDepth: number;
};
export declare const surfacePointAt: (config: SurfaceConfig, longitude: number, latitude: number) => Point3;
/** Project canonical face coordinates onto a primitive's front-facing sheet. */
export declare const surfaceFrontSampleAt: (config: SurfaceConfig, x: number, y: number) => SurfaceSample;
export declare const surfaceNormalAt: (config: SurfaceConfig, longitude: number, latitude: number) => Point3;
export declare const surfaceSampleAt: (config: SurfaceConfig, longitude: number, latitude: number) => SurfaceSample;
//# sourceMappingURL=surfaces.d.ts.map