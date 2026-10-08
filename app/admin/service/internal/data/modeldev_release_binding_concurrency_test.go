//go:build modeldev_pg

package data

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01"
	"go-wind-admin/app/admin/service/internal/data/ent/modeldevreleasebinding"
)

func TestModelDevReleaseBindingConcurrentChangesHaveOneWinner(t *testing.T) {
	baseContext, client, scope, initial := newModelDevBindingValidationFixture(t)
	repo := NewModelDevReleaseBindingRepo(client)
	first, err := repo.CompareAndSwap(baseContext, scope, initial)
	require.NoError(t, err)
	const contenders = 4
	updates := make([]ModelDevReleaseBindingUpdate, contenders)
	for i := range updates {
		updates[i] = initial
		updates[i].ExpectedGeneration = 1
		updates[i].Actor = fmt.Sprintf("governance:user:%d", 10+i)
		updates[i].Reason = fmt.Sprintf("concurrent target %d", i)
		updates[i].RequestedAt = initial.RequestedAt.Add(time.Duration(i+1) * time.Second)
		if i == 0 {
			updates[i].Target.NewSubmissionsEnabled = false
		} else {
			updates[i].Target.ReleaseID = uuid.NewString()
		}
	}
	type outcome struct {
		index  int
		change *ModelDevReleaseBindingChange
		err    error
	}
	ctx, cancel := context.WithTimeout(baseContext, 15*time.Second)
	var workers sync.WaitGroup
	defer func() {
		cancel()
		workers.Wait()
	}()
	start := make(chan struct{})
	results := make(chan outcome, contenders)
	for i := range updates {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			change, err := repo.CompareAndSwap(ctx, scope, updates[index])
			results <- outcome{index: index, change: change, err: err}
		}(i)
	}
	close(start)
	var winner *ModelDevReleaseBindingChange
	winnerIndex, successes := -1, 0
	for range contenders {
		select {
		case result := <-results:
			if result.err != nil {
				require.ErrorIs(t, result.err, ErrModelDevBindingGenerationConflict)
				require.Nil(t, result.change)
				continue
			}
			successes++
			winner, winnerIndex = result.change, result.index
		case <-ctx.Done():
			t.Fatal("bounded concurrent CAS did not finish")
		}
	}
	require.Equal(t, 1, successes)
	require.NotNil(t, winner)
	require.False(t, winner.Replayed)
	require.Equal(t, first.After, winner.Before)
	require.Equal(t, scope, winner.After.Scope)
	require.Equal(t, updates[winnerIndex].Target, winner.After.Target)
	require.Equal(t, uint64(2), winner.After.Generation)
	require.Equal(t, updates[winnerIndex].Actor, winner.After.UpdatedBy)
	require.Equal(t, updates[winnerIndex].RequestedAt, winner.After.UpdatedAt)
	require.Equal(t, updates[winnerIndex].Reason, winner.After.Reason)
	reader := NewModelDevReleaseBindingRepo(newModelDevPGClient(t))
	stored, err := reader.Get(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, winner.After, stored)
	replay, err := repo.CompareAndSwap(ctx, scope, updates[winnerIndex])
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	require.Equal(t, winner.After, replay.After)

	gate := updates[winnerIndex]
	gate.ExpectedGeneration = 2
	gate.Target.NewSubmissionsEnabled = !gate.Target.NewSubmissionsEnabled
	gate.Reason = "gate-only transition"
	gate.RequestedAt = gate.RequestedAt.Add(time.Minute)
	changed, err := repo.CompareAndSwap(ctx, scope, gate)
	require.NoError(t, err)
	require.False(t, changed.Replayed)
	require.Equal(t, winner.After, changed.Before)
	require.Equal(t, gate.Target, changed.After.Target)
	require.Equal(t, uint64(3), changed.After.Generation, "a gate change invalidates previously resolved admissions")
	replayedGate, err := reader.CompareAndSwap(ctx, scope, gate)
	require.NoError(t, err)
	require.True(t, replayedGate.Replayed)
	require.Equal(t, changed.After, replayedGate.After)
}

func TestModelDevReleaseBindingConcurrentTenantPresetIsolation(t *testing.T) {
	baseContext, firstClient, firstScope, first := newModelDevBindingValidationFixture(t)
	_, secondClient, secondScope, second := newModelDevBindingValidationFixture(t)
	otherPreset := firstScope
	otherPreset.PresetID = uuid.NewString()
	otherTarget := first
	otherTarget.Target.ReleaseID = uuid.NewString()
	second.Target.ReleaseID = uuid.NewString()
	groups := []struct {
		scope  ModelDevReleaseBindingScope
		update ModelDevReleaseBindingUpdate
		repo   *ModelDevReleaseBindingRepo
	}{
		{firstScope, first, NewModelDevReleaseBindingRepo(firstClient)},
		{otherPreset, otherTarget, NewModelDevReleaseBindingRepo(firstClient)},
		{secondScope, second, NewModelDevReleaseBindingRepo(secondClient)},
	}
	const contenders = 3
	type outcome struct {
		group  int
		change *ModelDevReleaseBindingChange
		err    error
	}
	ctx, cancel := context.WithTimeout(baseContext, 15*time.Second)
	var workers sync.WaitGroup
	defer func() {
		cancel()
		workers.Wait()
	}()
	start := make(chan struct{})
	results := make(chan outcome, len(groups)*contenders)
	for group := range groups {
		for contender := range contenders {
			workers.Add(1)
			go func(group, contender int) {
				defer workers.Done()
				<-start
				request := groups[group].update
				request.Actor = fmt.Sprintf("governance:user:%d", 20+contender)
				request.Reason = fmt.Sprintf("scope %d contender %d", group, contender)
				request.RequestedAt = request.RequestedAt.Add(time.Duration(contender) * time.Microsecond)
				change, err := groups[group].repo.CompareAndSwap(ctx, groups[group].scope, request)
				results <- outcome{group: group, change: change, err: err}
			}(group, contender)
		}
	}
	close(start)
	created := make([]int, len(groups))
	winners := make([]*ModelDevReleaseBinding, len(groups))
	completed := make([]outcome, 0, len(groups)*contenders)
	for range len(groups) * contenders {
		select {
		case result := <-results:
			require.NoError(t, result.err)
			require.NotNil(t, result.change)
			completed = append(completed, result)
			if result.change.Replayed {
				require.Equal(t, result.change.Before, result.change.After)
			} else {
				require.Nil(t, result.change.Before)
				created[result.group]++
				winners[result.group] = result.change.After
			}
		case <-ctx.Done():
			t.Fatal("bounded concurrent scope initialization did not finish")
		}
	}
	reader := NewModelDevReleaseBindingRepo(newModelDevPGClient(t))
	for i, group := range groups {
		require.Equal(t, 1, created[i])
		require.NotNil(t, winners[i])
		require.Equal(t, group.scope, winners[i].Scope)
		require.Equal(t, group.update.Target, winners[i].Target)
		require.Equal(t, uint64(1), winners[i].Generation)
		stored, err := reader.Get(ctx, group.scope)
		require.NoError(t, err)
		require.Equal(t, winners[i], stored)
	}
	for _, result := range completed {
		require.Equal(t, winners[result.group], result.change.After, "actor is audit data, not a separate default-binding scope")
	}
	forged := firstScope
	forged.ResourceTenantID = secondScope.ResourceTenantID
	stolen, err := reader.Get(ctx, forged)
	require.ErrorIs(t, err, ErrModelDevBindingNotFound)
	require.Nil(t, stolen)
	change, err := groups[0].repo.CompareAndSwap(ctx, forged, second)
	require.ErrorIs(t, err, cpup01.ErrInvalidArgument)
	require.Nil(t, change)
	for i, group := range groups {
		stored, err := reader.Get(ctx, group.scope)
		require.NoError(t, err)
		require.Equal(t, winners[i], stored, "a forged tenant/resource pair must not alter any real binding")
	}
	for _, expected := range []struct {
		tenant uint32
		count  int
	}{{firstScope.TenantID, 2}, {secondScope.TenantID, 1}} {
		count, err := firstClient.Client().ModelDevReleaseBinding.Query().Where(modeldevreleasebinding.TenantIDEQ(expected.tenant)).Count(ctx)
		require.NoError(t, err)
		require.Equal(t, expected.count, count)
	}
}
