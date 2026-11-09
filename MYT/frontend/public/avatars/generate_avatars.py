import json
import os

avatar_dir = os.path.dirname(os.path.abspath(__file__))

def make_character_lottie(name, body_color, hat_color, animation_type="idle"):
    fps = 24
    cx = 100
    w, h = 200, 220

    if animation_type == "idle":
        total_frames = 72
        head_py_kf = [
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":0,"s":[cx,68,0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":36,"s":[cx,62,0]},
            {"t":72,"s":[cx,68,0]}
        ]
        head_r_kf = None
        body_py_kf = [
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":0,"s":[cx,138,0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":36,"s":[cx,134,0]},
            {"t":72,"s":[cx,138,0]}
        ]
        la_py_kf = None; la_r_kf = None
        ra_py_kf = None; ra_r_kf = None
    elif animation_type == "working":
        total_frames = 48
        head_py_kf = [
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":0,"s":[cx,68,0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":12,"s":[cx,72,0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":24,"s":[cx,68,0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":36,"s":[cx,72,0]},
            {"t":48,"s":[cx,68,0]}
        ]
        head_r_kf = None
        body_py_kf = [{"k":[cx,138,0]}]
        la_py_kf = None; la_r_kf = None
        ra_py_kf = [
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":0,"s":[cx+26,120,0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":6,"s":[cx+26,116,0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":12,"s":[cx+26,120,0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":18,"s":[cx+28,117,0]},
            {"t":24,"s":[cx+26,120,0]}
        ]
        ra_r_kf = None
    elif animation_type == "thinking":
        total_frames = 72
        head_py_kf = [
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":0,"s":[cx,68,0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":24,"s":[cx+6,66,0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":48,"s":[cx,68,0]},
            {"t":72,"s":[cx+6,66,0]}
        ]
        head_r_kf = [
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":0,"s":[0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":24,"s":[12]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":48,"s":[0]},
            {"t":72,"s":[12]}
        ]
        body_py_kf = [{"k":[cx,138,0]}]
        la_py_kf = [
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":0,"s":[cx-26,120,0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":12,"s":[cx-10,78,0]},
            {"t":72,"s":[cx-10,78,0]}
        ]
        la_r_kf = [
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":0,"s":[0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":12,"s":[30]},
            {"t":72,"s":[30]}
        ]
        ra_py_kf = None; ra_r_kf = None
    elif animation_type == "celebrate":
        total_frames = 48
        head_py_kf = [
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":0,"s":[cx,68,0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":12,"s":[cx,48,0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":24,"s":[cx,68,0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":36,"s":[cx,48,0]},
            {"t":48,"s":[cx,68,0]}
        ]
        head_r_kf = None
        body_py_kf = [
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":0,"s":[cx,138,0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":12,"s":[cx,122,0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":24,"s":[cx,138,0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":36,"s":[cx,122,0]},
            {"t":48,"s":[cx,138,0]}
        ]
        la_py_kf = [
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":0,"s":[cx-26,120,0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":8,"s":[cx-38,76,0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":24,"s":[cx-26,120,0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":32,"s":[cx-38,76,0]},
            {"t":48,"s":[cx-26,120,0]}
        ]
        la_r_kf = [
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":0,"s":[0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":8,"s":[-45]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":24,"s":[0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":32,"s":[-45]},
            {"t":48,"s":[0]}
        ]
        ra_py_kf = [
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":0,"s":[cx+26,120,0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":8,"s":[cx+38,76,0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":24,"s":[cx+26,120,0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":32,"s":[cx+38,76,0]},
            {"t":48,"s":[cx+26,120,0]}
        ]
        ra_r_kf = [
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":0,"s":[0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":8,"s":[45]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":24,"s":[0]},
            {"i":{"x":0.667,"y":1},"o":{"x":0.333,"y":0},"t":32,"s":[45]},
            {"t":48,"s":[0]}
        ]
    else:
        total_frames = 72
        head_py_kf = [{"k":[cx,68,0]}]
        head_r_kf = None
        body_py_kf = [{"k":[cx,138,0]}]
        la_py_kf = None; la_r_kf = None
        ra_py_kf = None; ra_r_kf = None

    def mk_ks(py_kf, r_kf=None, static_p=None, static_r=0, static_s=None):
        ks = {"o": {"k": 100}}
        if py_kf and isinstance(py_kf, list) and len(py_kf) > 1:
            ks["p"] = {"a": {"k": py_kf}, "k": {"k": py_kf[0]["s"] if "s" in py_kf[0] else [cx,100,0]}}
        elif static_p:
            ks["p"] = {"k": {"k": static_p}}
        else:
            ks["p"] = {"k": {"k": [cx, 100, 0]}}
        if r_kf and isinstance(r_kf, list) and len(r_kf) > 1:
            ks["r"] = {"a": {"k": r_kf}, "k": {"k": r_kf[0]["s"] if "s" in r_kf[0] else 0}}
        else:
            ks["r"] = {"k": static_r}
        if static_s:
            ks["s"] = {"k": {"k": static_s}}
        else:
            ks["s"] = {"k": {"k": [100, 100, 100]}}
        ks["a"] = {"k": [0, 0, 0]}
        return ks

    # 桌子（working 状态才显示）
    table_layer = None
    if animation_type == "working":
        table_layer = {
            "ddd": 0, "ind": 0, "ty": 4, "nm": "Desk", "sr": 1,
            "ks": mk_ks(None, static_p=[cx, 180, 0]),
            "ao": 0,
            "shapes": [
                {"ty": "rc", "d": 1, "s": {"k": [90, 8]}, "p": {"k": [0, 0]}, "r": {"k": 2}, "nm": "DeskTop"},
                {"ty": "fl", "c": {"k": [0.55, 0.4, 0.3, 1]}, "o": {"k": 100}, "r": 1}
            ],
            "ip": 0, "op": total_frames, "st": 0
        }

    # 椅子
    chair_layer = {
        "ddd": 0, "ind": 1, "ty": 4, "nm": "Chair", "sr": 1,
        "ks": mk_ks(None, static_p=[cx, 168, 0]),
        "ao": 0,
        "shapes": [
            {"ty": "rc", "d": 1, "s": {"k": [55, 35]}, "p": {"k": [0, 0]}, "r": {"k": 6}, "nm": "ChairSeat"},
            {"ty": "fl", "c": {"k": [0.4, 0.4, 0.45, 1]}, "o": {"k": 100}, "r": 1}
        ],
        "ip": 0, "op": total_frames, "st": 0
    }

    # 左手臂
    la_ks = mk_ks(la_py_kf, la_r_kf, static_p=[cx-26, 120, 0])
    left_arm = {
        "ddd": 0, "ind": 2, "ty": 4, "nm": "Left Arm", "sr": 1,
        "ks": la_ks, "ao": 0,
        "shapes": [
            {"ty": "rc", "d": 1, "s": {"k": [10, 28]}, "p": {"k": [0, 0]}, "r": {"k": 5}, "nm": "Arm"},
            {"ty": "fl", "c": {"k": body_color}, "o": {"k": 100}, "r": 1}
        ],
        "ip": 0, "op": total_frames, "st": 0
    }

    # 右手臂
    ra_ks = mk_ks(ra_py_kf, ra_r_kf, static_p=[cx+26, 120, 0])
    right_arm = {
        "ddd": 0, "ind": 3, "ty": 4, "nm": "Right Arm", "sr": 1,
        "ks": ra_ks, "ao": 0,
        "shapes": [
            {"ty": "rc", "d": 1, "s": {"k": [10, 28]}, "p": {"k": [0, 0]}, "r": {"k": 5}, "nm": "Arm"},
            {"ty": "fl", "c": {"k": body_color}, "o": {"k": 100}, "r": 1}
        ],
        "ip": 0, "op": total_frames, "st": 0
    }

    # 身体
    body_ks = mk_ks(body_py_kf, static_p=[cx, 138, 0])
    body_layer = {
        "ddd": 0, "ind": 4, "ty": 4, "nm": "Body", "sr": 1,
        "ks": body_ks, "ao": 0,
        "shapes": [
            {"ty": "rc", "d": 1, "s": {"k": [38, 44]}, "p": {"k": [0, 0]}, "r": {"k": 8}, "nm": "BodyRect"},
            {"ty": "fl", "c": {"k": body_color}, "o": {"k": 100}, "r": 1}
        ],
        "ip": 0, "op": total_frames, "st": 0
    }

    # 头
    head_ks = mk_ks(head_py_kf, head_r_kf, static_p=[cx, 68, 0])
    mouth_h = 6 if animation_type != "celebrate" else 12
    head_layer = {
        "ddd": 0, "ind": 5, "ty": 4, "nm": "Head", "sr": 1,
        "ks": head_ks, "ao": 0,
        "shapes": [
            {"ty": "el", "p": {"k": [0, 0]}, "s": {"k": [72, 72]}, "nm": "HeadCircle"},
            {"ty": "fl", "c": {"k": [1, 0.87, 0.75, 1]}, "o": {"k": 100}, "r": 1},
            {"ty": "el", "p": {"k": [-12, -4]}, "s": {"k": [7, 9]}, "nm": "EyeL"},
            {"ty": "fl", "c": {"k": [0.15, 0.15, 0.15, 1]}, "o": {"k": 100}, "r": 1},
            {"ty": "el", "p": {"k": [12, -4]}, "s": {"k": [7, 9]}, "nm": "EyeR"},
            {"ty": "fl", "c": {"k": [0.15, 0.15, 0.15, 1]}, "o": {"k": 100}, "r": 1},
            {"ty": "el", "p": {"k": [0, 14]}, "s": {"k": [10, mouth_h]}, "nm": "Mouth"},
            {"ty": "fl", "c": {"k": [0.85, 0.35, 0.3, 1]}, "o": {"k": 100}, "r": 1},
        ],
        "ip": 0, "op": total_frames, "st": 0
    }

    # 帽子/头发（跟随头的运动）
    hair_ks = mk_ks(head_py_kf, head_r_kf, static_p=[cx, 68, 0])
    hair_layer = {
        "ddd": 0, "ind": 6, "ty": 4, "nm": "Hair", "sr": 1,
        "ks": hair_ks, "ao": 0,
        "shapes": [
            {"ty": "rc", "d": 1, "s": {"k": [76, 16]}, "p": {"k": [0, -38]}, "r": {"k": 6}, "nm": "Hat"},
            {"ty": "fl", "c": {"k": hat_color}, "o": {"k": 100}, "r": 1}
        ],
        "ip": 0, "op": total_frames, "st": 0
    }

    layers = [hair_layer, head_layer, body_layer, right_arm, left_arm, chair_layer]
    if table_layer:
        layers.append(table_layer)

    lottie = {
        "v": "5.12.1",
        "fr": fps,
        "ip": 0,
        "op": total_frames,
        "w": w,
        "h": h,
        "nm": f"{name}-{animation_type}",
        "ddd": 0,
        "assets": [],
        "layers": layers
    }
    return lottie


# 角色配置：名称、身体颜色、帽子颜色
characters = [
    ("dev",      [0.29, 0.56, 1.0, 1],  [0.18, 0.36, 0.75, 1]),   # 蓝 - 研发
    ("ops",      [1.0, 0.42, 0.21, 1],  [0.78, 0.32, 0.12, 1]),   # 橙 - 运维
    ("design",   [0.58, 0.22, 0.69, 1],  [0.42, 0.12, 0.55, 1]),   # 紫 - 设计
    ("security", [0.3, 0.3, 0.35, 1],    [0.15, 0.15, 0.18, 1]),   # 深灰 - 安全
    ("data",     [0.22, 0.73, 0.43, 1],  [0.12, 0.55, 0.28, 1]),   # 绿 - 数据
    ("test",     [0.85, 0.72, 0.2, 1],   [0.68, 0.55, 0.1, 1]),    # 黄 - 测试
]

animations = ["idle", "working", "thinking", "celebrate"]

files_created = []
for name, body_c, hat_c in characters:
    for anim in animations:
        lottie = make_character_lottie(name, body_c, hat_c, anim)
        filename = f"{name}-{anim}.json"
        filepath = os.path.join(avatar_dir, filename)
        with open(filepath, 'w', encoding='utf-8') as f:
            json.dump(lottie, f, separators=(',', ':'))
        files_created.append(filename)

print(f"Created {len(files_created)} animation files in {avatar_dir}:")
total_size = 0
for f in sorted(files_created):
    fpath = os.path.join(avatar_dir, f)
    size = os.path.getsize(fpath)
    total_size += size
    print(f"  {f:30s} {size:>6,} bytes")
print(f"\nTotal: {total_size:,} bytes ({total_size/1024:.1f} KB)")
