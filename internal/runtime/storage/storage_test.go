package storage

import (
	"testing"

	"github.com/rbroggi/grpcmock/internal/runtime"
	"github.com/stretchr/testify/require"
)

func TestCreateAndGetExpectation(t *testing.T) {
	store := New()
	exp := runtime.GRPCCallExpectation{
		FullMethodName: "/test.Service/Method",
		Response:       &runtime.MockResponse{Body: []byte(`{"foo":"bar"}`)},
	}
	id, err := store.CreateExpectation(exp)
	require.NoError(t, err)
	require.NotEmpty(t, id)
	got, ok := store.GetExpectationByID(id)
	require.True(t, ok)
	require.Equal(t, exp.FullMethodName, got.FullMethodName)
}

func TestCreateExpectation_Duplicate(t *testing.T) {
	store := New()
	exp := runtime.GRPCCallExpectation{
		FullMethodName: "/test.Service/Method",
		Response:       &runtime.MockResponse{Body: []byte(`{"foo":"bar"}`)},
	}
	id1, err1 := store.CreateExpectation(exp)
	require.NoError(t, err1)
	id2, err2 := store.CreateExpectation(exp)
	require.Error(t, err2)
	require.Equal(t, id1, id2)
}

func TestListExpectations_FilterByFullMethodName(t *testing.T) {
	store := New()
	exp1 := runtime.GRPCCallExpectation{
		FullMethodName: "/foo.Bar/Baz",
		Response:       &runtime.MockResponse{Body: []byte(`{"foo":1}`)},
	}
	exp2 := runtime.GRPCCallExpectation{
		FullMethodName: "/foo.Bar/Other",
		Response:       &runtime.MockResponse{Body: []byte(`{"foo":2}`)},
	}
	store.CreateExpectation(exp1)
	store.CreateExpectation(exp2)
	all := store.ListExpectations(runtime.ListExpectationsOptions{})
	require.Len(t, all, 2)
	filtered := store.ListExpectations(runtime.ListExpectationsOptions{FullMethodName: "/foo.Bar/Baz"})
	require.Len(t, filtered, 1)
	require.Equal(t, "/foo.Bar/Baz", filtered[0].FullMethodName)
}

func TestDeleteExpectation(t *testing.T) {
	store := New()
	exp := runtime.GRPCCallExpectation{
		FullMethodName: "/test.Service/Delete",
		Response:       &runtime.MockResponse{Body: []byte(`{"foo":"bar"}`)},
	}
	id, err := store.CreateExpectation(exp)
	require.NoError(t, err)
	deleted := store.DeleteExpectation(id)
	require.True(t, deleted, "expectation should be deleted")
	_, ok := store.GetExpectationByID(id)
	require.False(t, ok, "expectation should not be found after deletion")
	// Deleting again should return false
	deletedAgain := store.DeleteExpectation(id)
	require.False(t, deletedAgain, "deleting non-existent expectation should return false")
}

func TestClearExpectations(t *testing.T) {
	store := New()
	exp1 := runtime.GRPCCallExpectation{
		FullMethodName: "/foo.Bar/Baz",
		Response:       &runtime.MockResponse{Body: []byte(`{"foo":1}`)},
	}
	exp2 := runtime.GRPCCallExpectation{
		FullMethodName: "/foo.Bar/Other",
		Response:       &runtime.MockResponse{Body: []byte(`{"foo":2}`)},
	}
	store.CreateExpectation(exp1)
	store.CreateExpectation(exp2)
	all := store.ListExpectations(runtime.ListExpectationsOptions{})
	require.Len(t, all, 2)
	store.ClearExpectations()
	allAfterClear := store.ListExpectations(runtime.ListExpectationsOptions{})
	require.Len(t, allAfterClear, 0)
}

func TestIncrementMatch(t *testing.T) {
	store := New()
	exp := runtime.GRPCCallExpectation{
		FullMethodName: "/test.Service/Increment",
		Response:       &runtime.MockResponse{Body: []byte(`{"foo":"bar"}`)},
	}
	id, err := store.CreateExpectation(exp)
	require.NoError(t, err)
	for i := 1; i <= 3; i++ {
		store.IncrementMatch(id)
		require.Equal(t, i, store.matchCounts[id])
	}
}

func TestGetMatches(t *testing.T) {
	store := New()
	exp := runtime.GRPCCallExpectation{
		FullMethodName: "/test.Service/GetMatches",
		Response:       &runtime.MockResponse{Body: []byte(`{"foo":"bar"}`)},
	}
	id, err := store.CreateExpectation(exp)
	require.NoError(t, err)
	require.Equal(t, 0, store.GetMatches(id))
	store.IncrementMatch(id)
	require.Equal(t, 1, store.GetMatches(id))
	store.IncrementMatch(id)
	require.Equal(t, 2, store.GetMatches(id))
}
