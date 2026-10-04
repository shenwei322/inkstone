package service

import "testing"

func TestFilterMergeSources(t *testing.T) {
	cases := []struct {
		name      string
		targetID  uint
		sourceIDs []uint
		want      []uint
	}{
		{
			name:      "丢弃 0",
			targetID:  1,
			sourceIDs: []uint{0, 5},
			want:      []uint{5},
		},
		{
			name:      "丢弃目标自身（常见误操作）",
			targetID:  7,
			sourceIDs: []uint{7, 3, 4},
			want:      []uint{3, 4},
		},
		{
			name:      "去重",
			targetID:  1,
			sourceIDs: []uint{2, 2, 3, 3, 3},
			want:      []uint{2, 3},
		},
		{
			name:      "全被过滤时返回空",
			targetID:  9,
			sourceIDs: []uint{0, 9},
			want:      []uint{},
		},
		{
			name:      "空输入返回空切片而非 nil",
			targetID:  1,
			sourceIDs: nil,
			want:      []uint{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := filterMergeSources(tc.targetID, tc.sourceIDs)
			if len(got) != len(tc.want) {
				t.Fatalf("filterMergeSources(%d, %v) = %v，想要 %v", tc.targetID, tc.sourceIDs, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("filterMergeSources(%d, %v) = %v，想要 %v", tc.targetID, tc.sourceIDs, got, tc.want)
				}
			}
		})
	}
}

// TestMergeTagsValidation 只测 service 层的输入校验分支
// （不碰 repository，因此不会真的执行 SQL）。
func TestMergeTagsValidation(t *testing.T) {
	t.Run("目标为 0 报错", func(t *testing.T) {
		if _, err := (&TaxonomyService{}).MergeTags(0, []uint{1}); err == nil {
			t.Error("target_id = 0 应报错")
		}
	})

	t.Run("来源被全部过滤时报错", func(t *testing.T) {
		if _, err := (&TaxonomyService{}).MergeTags(5, []uint{0, 5}); err == nil {
			t.Error("来源只含目标标签时应提示重新选择")
		}
	})

	t.Run("为空来源报错", func(t *testing.T) {
		if _, err := (&TaxonomyService{}).MergeTags(5, nil); err == nil {
			t.Error("nil 来源应报错")
		}
	})

	t.Run("超过 50 个来源拒绝", func(t *testing.T) {
		// 51 个互不相同的 id，且都不等于 targetID，确保真的超限
		many := make([]uint, 0, maxMergeTags+1)
		for i := uint(0); i < maxMergeTags+1; i++ {
			many = append(many, i+100) // 100 起，避开 targetID=1
		}
		if _, err := (&TaxonomyService{}).MergeTags(1, many); err == nil {
			t.Errorf("超过 %d 个应拒绝（事务持锁过久）", maxMergeTags)
		}
	})
}

// TestMergeTagsSourceFilteringPreventsAccidentalDrop 守住一条容易回归的规则：
// 管理员把目标标签也勾进来源，不能被判为"没有可合并的标签"而拒绝整批操作。
func TestMergeTagsSourceFilteringPreventsAccidentalDrop(t *testing.T) {
	got := filterMergeSources(3, []uint{7, 3, 3, 8, 0})
	if len(got) != 2 || got[0] != 7 || got[1] != 8 {
		t.Fatalf("应保留 7 与 8、剔除目标 3 与无效 0，实际 %v", got)
	}
}
