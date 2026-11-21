package onebot

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	zero "github.com/wdvxdr1123/ZeroBot"

	"plumebot/internal/domain"
)

const groupMemberListTimeout = 3 * time.Second

type eventGroupMemberReader struct {
	ctx     *zero.Ctx
	groupID int64
}

func newGroupMemberReader(ctx *zero.Ctx) domain.GroupMemberReader {
	if ctx == nil || ctx.Event == nil || ctx.Event.GroupID <= 0 {
		return nil
	}
	return &eventGroupMemberReader{ctx: ctx, groupID: ctx.Event.GroupID}
}

func (r *eventGroupMemberReader) ListCurrentMembers(ctx context.Context) ([]string, error) {
	if r == nil || r.ctx == nil || r.groupID <= 0 {
		return nil, errors.New("当前事件不是有效群聊")
	}
	callCtx, cancel := context.WithTimeout(ctx, groupMemberListTimeout)
	defer cancel()
	rsp := r.ctx.CallActionWithContext(callCtx, "get_group_member_list", zero.Params{
		"group_id": r.groupID,
		"no_cache": true,
	})
	if rsp.Status != "ok" || rsp.RetCode != 0 {
		return nil, fmt.Errorf("get_group_member_list 失败: status=%s retcode=%d", rsp.Status, rsp.RetCode)
	}
	if !rsp.Data.IsArray() {
		return nil, errors.New("get_group_member_list 返回的数据不是成员数组")
	}
	items := rsp.Data.Array()
	if len(items) == 0 {
		return nil, errors.New("get_group_member_list 返回空成员数组")
	}
	members := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		id := item.Get("user_id").String()
		parsed, err := strconv.ParseInt(id, 10, 64)
		if err != nil || parsed <= 0 {
			return nil, errors.New("get_group_member_list 包含无效成员 ID")
		}
		id = strconv.FormatInt(parsed, 10)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		members = append(members, id)
	}
	if len(members) == 0 {
		return nil, errors.New("get_group_member_list 没有有效成员")
	}
	return members, nil
}
