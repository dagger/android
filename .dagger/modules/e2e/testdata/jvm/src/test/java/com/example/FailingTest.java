package com.example;

import static org.junit.jupiter.api.Assertions.fail;
import org.junit.jupiter.api.Test;

// Deliberately fails: discovery and a passing subset must never execute it.
public class FailingTest {
    @Test
    void failsWhenExecuted() {
        fail("fixture failure proves selected tests actually execute");
    }
}
