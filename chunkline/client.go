package chunkline

import (
	"container/heap"
	"context"
	"slices"
	"sort"
	"time"
)

// QueueItem is used internally by GetRecentItems for the priority queue.
type QueueItem struct {
	Timeline string
	Item     BodyItem
	Index    int
}

// PriorityQueue implements heap.Interface for QueueItem.
type PriorityQueue []*QueueItem

func (pq PriorityQueue) Len() int { return len(pq) }
func (pq PriorityQueue) Less(i, j int) bool {
	return pq[i].Item.Timestamp.After(pq[j].Item.Timestamp)
}
func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}
func (pq *PriorityQueue) Push(x any) {
	item := x.(*QueueItem)
	*pq = append(*pq, item)
}
func (pq *PriorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[0 : n-1]
	return item
}

type Client struct {
	resolver Resolver
}

func NewClient(resolver Resolver) *Client {
	return &Client{
		resolver: resolver,
	}
}

func (s *Client) QueryDescending(ctx context.Context, uris []string, until time.Time, limit int) ([]BodyItemWithSource, error) {

	manifests, err := s.resolver.ResolveTimelines(ctx, uris)
	if err != nil {
		return nil, err
	}

	cancelMap, err := s.resolver.GetRemovedItems(ctx, uris)
	if err != nil {
		return nil, err
	}

	chunks, err := s.getChunks(ctx, uris, until)
	if err != nil {
		return nil, err
	}

	pq := make(PriorityQueue, 0)
	heap.Init(&pq)

	for timeline, chunk := range chunks {

		if len(chunk.Items) <= 0 {
			continue
		}

		index := sort.Search(len(chunk.Items), func(i int) bool {
			return chunk.Items[i].Timestamp.Before(until)
		})

		if index >= len(chunk.Items) {
			continue
		}

		heap.Push(&pq, &QueueItem{
			Timeline: timeline,
			Item:     chunk.Items[index],
			Index:    index,
		})
	}

	var result []BodyItemWithSource
	var uniq = make(map[string]bool)

	var itrlimit = 1000
	for len(result) < limit && pq.Len() > 0 && itrlimit > 0 {
		itrlimit--
		smallest := heap.Pop(&pq).(*QueueItem)
		_, exists := uniq[smallest.Item.ID()]
		retracted := false
		cancelList, ok := cancelMap[smallest.Timeline]
		if ok {
			retracted = slices.Contains(cancelList, smallest.Item.ID())
		}

		if !exists && !retracted {
			result = append(result, BodyItemWithSource{
				BodyItem: smallest.Item,
				Source:   smallest.Timeline,
			})
			uniq[smallest.Item.ID()] = true
		}

		nextIndex := smallest.Index + 1
		timeline := smallest.Timeline

		if nextIndex < len(chunks[timeline].Items) {
			heap.Push(&pq, &QueueItem{
				Timeline: timeline,
				Item:     chunks[timeline].Items[nextIndex],
				Index:    nextIndex,
			})
		} else {
			prevChunkId := manifests[timeline].Time2Chunk(smallest.Item.Timestamp)
			if prevChunkId == chunks[timeline].ChunkID {
				prevChunkId--
			}

			prevChunks, err := s.getChunks(ctx, []string{timeline}, manifests[timeline].Chunk2Time(prevChunkId))
			if err != nil {
				continue
			}
			if prevChunk, ok := prevChunks[timeline]; ok {
				if len(prevChunk.Items) <= 0 {
					continue
				}
				chunks[timeline] = prevChunk
				heap.Push(&pq, &QueueItem{
					Timeline: timeline,
					Item:     prevChunk.Items[0],
					Index:    0,
				})
			}
		}
	}

	return result, nil
}

func (s *Client) getChunks(ctx context.Context, uris []string, until time.Time) (map[string]BodyChunk, error) {
	query, err := s.resolver.LookupChunkItrs(ctx, uris, until)
	if err != nil {
		return nil, err
	}

	chunks, err := s.resolver.LoadChunkBodies(ctx, query)
	if err != nil {
		return nil, err
	}

	return chunks, nil
}
