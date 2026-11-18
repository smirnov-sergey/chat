package handlers

import "context"

type userIDContextKey struct{}

func userIDFromContext(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(userIDContextKey{}).(int64)
	return id, ok
}
