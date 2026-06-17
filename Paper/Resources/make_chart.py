import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np

plt.rcParams.update({
    "font.family": "serif",
    "font.serif": ["Times New Roman", "DejaVu Serif"],
    "font.size": 9,
    "axes.linewidth": 0.6,
})

# Mean deviation from nominal (mm)
metrics = ["cube\nouter", "Hole\nouter", "10 mm\nhole", "20 mm\nhole"]
cura_matched = [0.06, 0.08, -0.05, -0.07]
vibe         = [0.06, 0.07, -0.10, -0.08]
cura_opt     = [-0.01, 0.03, -0.01, -0.04]

x = np.arange(len(metrics))
w = 0.26

fig, ax = plt.subplots(figsize=(3.4, 2.5))

b1 = ax.bar(x - w, cura_matched, w, label="Cura matched",
            color="#4878a8", edgecolor="black", linewidth=0.4)
b2 = ax.bar(x, vibe, w, label="Vibe Slicer",
            color="#d98c3f", edgecolor="black", linewidth=0.4)
b3 = ax.bar(x + w, cura_opt, w, label="Cura opt. (ref. only)",
            color="#bdbdbd", edgecolor="black", linewidth=0.4, hatch="///")

ax.axhline(0, color="black", linewidth=0.6)
ax.set_ylabel("Mean deviation from\nnominal (mm)")
ax.set_xticks(x)
ax.set_xticklabels(metrics)
ax.set_ylim(-0.18, 0.16)
ax.legend(loc="lower left", fontsize=6.5, framealpha=0.9)
ax.grid(axis="y", linestyle=":", linewidth=0.4, alpha=0.6)
ax.tick_params(width=0.5)

plt.tight_layout(pad=0.4)
plt.savefig("/home/claude/bench_deviation.pdf", bbox_inches="tight")
print("deviation chart done")

# ---- print time chart ----
fig2, ax2 = plt.subplots(figsize=(3.4, 2.2))
objs = ["cube_10", "Hole_Structure"]
cm_t  = [9 + 4/60, 28 + 33/60]
vibe_t= [7 + 47/60, 46 + 12/60]
opt_t = [11 + 32/60, 31 + 47/60]

x2 = np.arange(len(objs))
b1 = ax2.bar(x2 - w, cm_t, w, label="Cura matched",
             color="#4878a8", edgecolor="black", linewidth=0.4)
b2 = ax2.bar(x2, vibe_t, w, label="Vibe Slicer",
             color="#d98c3f", edgecolor="black", linewidth=0.4)
b3 = ax2.bar(x2 + w, opt_t, w, label="Cura opt. (ref. only)",
             color="#bdbdbd", edgecolor="black", linewidth=0.4, hatch="///")
ax2.set_ylabel("Mean print time (min)")
ax2.set_xticks(x2)
ax2.set_xticklabels(objs)
ax2.legend(loc="upper left", fontsize=6.5, framealpha=0.9)
ax2.grid(axis="y", linestyle=":", linewidth=0.4, alpha=0.6)
ax2.tick_params(width=0.5)
plt.tight_layout(pad=0.4)
plt.savefig("/home/claude/bench_time.pdf", bbox_inches="tight")
print("time chart done")
