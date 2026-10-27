package com.example;

import static org.junit.jupiter.api.Assertions.assertEquals;
import org.junit.jupiter.api.Test;

public class PassingTest {
    @Test
    void addsNumbers() {
        assertEquals(4, 2 + 2);
    }
}
