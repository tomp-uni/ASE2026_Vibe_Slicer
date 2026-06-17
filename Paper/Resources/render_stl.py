import struct
import numpy as np
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
from mpl_toolkits.mplot3d.art3d import Poly3DCollection

def read_binary_stl(path):
    with open(path, "rb") as f:
        f.read(80)  # header
        (n,) = struct.unpack("<I", f.read(4))
        tris = []
        for _ in range(n):
            data = f.read(50)
            # 12 floats: normal(3) + v1(3) + v2(3) + v3(3), then 2-byte attr
            vals = struct.unpack("<12f", data[:48])
            v1 = vals[3:6]; v2 = vals[6:9]; v3 = vals[9:12]
            tris.append([v1, v2, v3])
        return np.array(tris)

def render(path, out, elev=22, azim=-50, title="", true_aspect=False,
           z_ticks=None, right_margin=0.86):
    tris = read_binary_stl(path)
    fig = plt.figure(figsize=(3.6, 3.2))
    ax = fig.add_subplot(111, projection="3d")

    coll = Poly3DCollection(tris, alpha=1.0)
    coll.set_facecolor((0.62, 0.70, 0.82))
    coll.set_edgecolor((0.20, 0.28, 0.40))
    coll.set_linewidth(0.25)
    ax.add_collection3d(coll)

    # bounds
    pts = tris.reshape(-1, 3)
    mins = pts.min(axis=0); maxs = pts.max(axis=0)
    size = maxs - mins
    ctr = (mins + maxs) / 2

    if true_aspect:
        pad = size.max() * 0.04
        ax.set_xlim(mins[0]-pad, maxs[0]+pad)
        ax.set_ylim(mins[1]-pad, maxs[1]+pad)
        ax.set_zlim(mins[2]-pad, maxs[2]+pad)
        try:
            ax.set_box_aspect((size[0], size[1], size[2]))
        except Exception:
            pass
    else:
        span = size.max() / 2 * 1.05
        ax.set_xlim(ctr[0]-span, ctr[0]+span)
        ax.set_ylim(ctr[1]-span, ctr[1]+span)
        ax.set_zlim(ctr[2]-span, ctr[2]+span)
        try:
            ax.set_box_aspect((1, 1, 1))
        except Exception:
            pass

    ax.view_init(elev=elev, azim=azim)
    ax.set_xlabel("X (mm)", fontsize=7, labelpad=1)
    ax.set_ylabel("Y (mm)", fontsize=7, labelpad=1)
    ax.set_zlabel("Z (mm)", fontsize=7, labelpad=1)
    ax.tick_params(labelsize=6, pad=1)

    # For very thin objects, force a sensible number of Z ticks so the
    # auto-locator does not stack many labels into an unreadable blob.
    if z_ticks is not None:
        ax.set_zticks(z_ticks)

    ax.grid(True)
    # Controlled margin instead of bbox_inches='tight' so axis labels
    # are never clipped at the figure edge.
    fig.subplots_adjust(left=0.02, right=right_margin, bottom=0.06, top=0.98)
    plt.savefig(out, dpi=200)
    plt.close()
    print(f"{path}: {len(tris)} triangles -> {out}, bounds {mins} .. {maxs}")

render("cube_10.stl", "cube_render.png", elev=20, azim=-55)
render("Hole_Structure.stl", "hole_render.png", elev=42, azim=-60,
       true_aspect=True, z_ticks=[0, 5], right_margin=0.78)

